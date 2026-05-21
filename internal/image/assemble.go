// Package image — layer-lərdən rootfs yığma.
//
// OCI image bir neçə layer-dən ibarətdir. Hər layer əvvəlkinin üstündə
// tətbiq olunur. Layer-də xüsusi "whiteout" faylları silinmiş fayl-ları
// işarələyir (".wh." prefiksi ilə).
package image

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// AssembleRootFS — image-in layer-lərini birləşdirib rootfs yaradır.
//
// Path: /var/lib/azcontainer/images/<name>/rootfs/
//
// Hər layer ardıcıllıqla açılır, üst layer alt layer-i override edir.
// Whiteout (.wh.X) faylları "X-i sil" deməkdir.
func (s *Store) AssembleRootFS(imageName string) error {
	manifest, _, err := s.LoadImage(imageName)
	if err != nil {
		return fmt.Errorf("image yüklə: %w", err)
	}

	rootfsPath := s.ImageRootFS(imageName)

	// Əvvəlcədən mövcud rootfs varsa sil (təmiz başlayaq).
	if _, err := os.Stat(rootfsPath); err == nil {
		if err := os.RemoveAll(rootfsPath); err != nil {
			return fmt.Errorf("köhnə rootfs sil: %w", err)
		}
	}

	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		return fmt.Errorf("rootfs dir: %w", err)
	}

	// Hər layer-i sıra ilə extract et.
	for i, layer := range manifest.Layers {
		fmt.Printf("Layer %d/%d extract: %s\n", i+1, len(manifest.Layers), shortDigest(layer.Digest))
		if err := s.extractLayer(layer, rootfsPath); err != nil {
			return fmt.Errorf("layer %d extract: %w", i, err)
		}
	}

	fmt.Printf("✓ Rootfs hazır: %s\n", rootfsPath)
	return nil
}

// extractLayer — bir layer-i target qovluğa açır.
func (s *Store) extractLayer(layer Descriptor, target string) error {
	blobPath := s.BlobPath(layer.Digest)
	f, err := os.Open(blobPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// MediaType-a görə gzip olub-olmadığını yoxla.
	var reader io.Reader = f
	if strings.Contains(layer.MediaType, "gzip") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	return extractLayerTar(reader, target)
}

// extractLayerTar — tar stream-ini target-ə açır, whiteout-ları emal edir.
func extractLayerTar(r io.Reader, target string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Path traversal qoruması.
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			continue
		}

		// Whiteout emalı — OCI standartı.
		base := filepath.Base(cleanName)
		dir := filepath.Dir(cleanName)

		if base == ".wh..wh..opq" {
			// Opaque whiteout — bu qovluğun bütün içindəkilərini sil.
			targetDir := filepath.Join(target, dir)
			if entries, err := os.ReadDir(targetDir); err == nil {
				for _, e := range entries {
					os.RemoveAll(filepath.Join(targetDir, e.Name()))
				}
			}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			// Normal whiteout — ".wh.X" → "X"-i sil.
			realName := strings.TrimPrefix(base, ".wh.")
			os.RemoveAll(filepath.Join(target, dir, realName))
			continue
		}

		path := filepath.Join(target, cleanName)
		if err := applyTarEntry(hdr, tr, path); err != nil {
			return fmt.Errorf("entry %s: %w", cleanName, err)
		}
	}
	return nil
}

// applyTarEntry — bir tar entry-ni diskə yazır.
func applyTarEntry(hdr *tar.Header, r io.Reader, path string) error {
	mode := os.FileMode(hdr.Mode)

	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(path, mode); err != nil {
			return err
		}

	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		// Köhnə fayl varsa sil (override).
		os.Remove(path)
		out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, r); err != nil {
			out.Close()
			return err
		}
		out.Close()

	case tar.TypeSymlink:
		os.MkdirAll(filepath.Dir(path), 0755)
		os.Remove(path)
		if err := os.Symlink(hdr.Linkname, path); err != nil {
			return err
		}

	case tar.TypeLink:
		// Hard link — target köhnə fayl olmalıdır.
		os.MkdirAll(filepath.Dir(path), 0755)
		os.Remove(path)
		linkTarget := filepath.Join(filepath.Dir(path), hdr.Linkname)
		// Mütləq target-i həll et.
		if filepath.IsAbs(hdr.Linkname) {
			linkTarget = hdr.Linkname
		}
		if err := os.Link(linkTarget, path); err != nil {
			// Hard link uğursuz olarsa, fayl copy edək (best effort).
			if data, readErr := os.ReadFile(linkTarget); readErr == nil {
				os.WriteFile(path, data, 0644)
			}
		}

	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		// Device və FIFO-ları atırıq (container daxilində /dev tmpfs-dir).
		return nil
	}

	// Ownership saxla (root olduğumuz üçün işləyəcək).
	if hdr.Typeflag != tar.TypeSymlink {
		os.Chown(path, hdr.Uid, hdr.Gid)
	}
	return nil
}
