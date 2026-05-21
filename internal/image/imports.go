// Package image — docker save formatı import.
package image

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ImportDockerSave — `docker save` ilə yaradılmış tar faylını import edir.
//
// Tar daxilində:
//
//	manifest.json                    ← []dockerManifest
//	<config_hash>.json               ← OCI config
//	<layer_hash>/layer.tar           ← uncompressed layer (Docker köhnə)
//	blobs/sha256/<hash>              ← OCI yeni format
//
// İki format-ı da dəstəkləyirik.
func (s *Store) ImportDockerSave(tarPath, imageName string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("tar aç: %w", err)
	}
	defer f.Close()

	// ADDIM 1: Tar-ı müvəqqəti qovluğa açırıq.
	tmpDir, err := os.MkdirTemp("", "azc-import-*")
	if err != nil {
		return fmt.Errorf("tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTar(f, tmpDir); err != nil {
		return fmt.Errorf("tar extract: %w", err)
	}

	// ADDIM 2: manifest.json oxu (kök).
	manifestPath := filepath.Join(tmpDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("manifest.json oxu: %w", err)
	}

	var dockerManifests []dockerManifest
	if err := json.Unmarshal(manifestData, &dockerManifests); err != nil {
		return fmt.Errorf("manifest parse: %w", err)
	}
	if len(dockerManifests) == 0 {
		return fmt.Errorf("manifest.json boşdur")
	}
	dm := dockerManifests[0]

	// ADDIM 3: Config-i oxu və blob kimi yaz.
	configPath := filepath.Join(tmpDir, dm.Config)
	configFile, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("config aç: %w", err)
	}
	configDigest, err := s.WriteBlob(configFile, "")
	configFile.Close()
	if err != nil {
		return fmt.Errorf("config blob: %w", err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config oxu: %w", err)
	}
	config := &Config{}
	if err := json.Unmarshal(configBytes, config); err != nil {
		return fmt.Errorf("config parse: %w", err)
	}

	configStat, _ := os.Stat(configPath)

	// ADDIM 4: Layer-ləri blob kimi yaz.
	layers := make([]Descriptor, 0, len(dm.Layers))
	for _, layerPath := range dm.Layers {
		fullPath := filepath.Join(tmpDir, layerPath)
		layerFile, err := os.Open(fullPath)
		if err != nil {
			return fmt.Errorf("layer aç (%s): %w", layerPath, err)
		}
		stat, _ := layerFile.Stat()

		digest, err := s.WriteBlob(layerFile, "")
		layerFile.Close()
		if err != nil {
			return fmt.Errorf("layer blob: %w", err)
		}

		// Docker save layer-ləri uncompressed verir (.tar).
		mediaType := "application/vnd.oci.image.layer.v1.tar"
		if strings.HasSuffix(layerPath, ".gz") {
			mediaType = "application/vnd.oci.image.layer.v1.tar+gzip"
		}

		layers = append(layers, Descriptor{
			MediaType: mediaType,
			Digest:    digest,
			Size:      stat.Size(),
		})
	}

	// ADDIM 5: Manifest qur və saxla.
	manifest := &Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    configDigest,
			Size:      configStat.Size(),
		},
		Layers: layers,
	}

	if err := s.SaveImage(imageName, manifest, config); err != nil {
		return fmt.Errorf("image saxla: %w", err)
	}

	fmt.Printf("Image import edildi: %s (%d layer)\n", imageName, len(layers))
	return nil
}

// ImportRootfsTar — sadə tar.gz rootfs-i image kimi import edir.
//
// Bu, "docker save" deyil — adi rootfs tar. Test üçün rahatdır:
//
//	tar -czf alpine-rootfs.tar.gz -C /var/lib/azcontainer/images/alpine .
//	azcontainer import-rootfs alpine-rootfs.tar.gz alpine
//
// Synthetic config yaradırıq — yalnız bir layer.
func (s *Store) ImportRootfsTar(tarPath, imageName string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("tar aç: %w", err)
	}
	defer f.Close()

	// Layer-i blob kimi yaz.
	layerDigest, err := s.WriteBlob(f, "")
	if err != nil {
		return fmt.Errorf("layer blob: %w", err)
	}

	stat, _ := os.Stat(tarPath)

	// Detect format (gzip vs raw tar).
	mediaType := "application/vnd.oci.image.layer.v1.tar+gzip"
	if !strings.HasSuffix(tarPath, ".gz") && !strings.HasSuffix(tarPath, ".tgz") {
		mediaType = "application/vnd.oci.image.layer.v1.tar"
	}

	// Synthetic config.
	config := &Config{
		Architecture: "amd64",
		OS:           "linux",
		Config: ImageConfig{
			Env: []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			},
			Cmd:        []string{"/bin/sh"},
			WorkingDir: "/",
		},
		RootFS: RootFS{
			Type:    "layers",
			DiffIDs: []string{layerDigest},
		},
	}

	configBytes, _ := json.Marshal(config)
	configDigest, err := s.WriteBlob(strings.NewReader(string(configBytes)), "")
	if err != nil {
		return fmt.Errorf("config blob: %w", err)
	}

	manifest := &Manifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: Descriptor{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    configDigest,
			Size:      int64(len(configBytes)),
		},
		Layers: []Descriptor{
			{MediaType: mediaType, Digest: layerDigest, Size: stat.Size()},
		},
	}

	if err := s.SaveImage(imageName, manifest, config); err != nil {
		return fmt.Errorf("image saxla: %w", err)
	}

	fmt.Printf("Rootfs import edildi: %s\n", imageName)
	return nil
}

// extractTar — tar-ı target qovluğuna açır (path traversal qoruması ilə).
func extractTar(r io.Reader, target string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Path traversal qoruması — "../" attack.
		cleanPath := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			continue // təhlükəli path-i atla
		}
		path := filepath.Join(target, cleanPath)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			out, err := os.Create(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(path), 0755)
			os.Symlink(hdr.Linkname, path)
		}
	}
	return nil
}
