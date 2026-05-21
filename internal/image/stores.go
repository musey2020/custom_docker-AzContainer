// Package image — lokal image store.
//
// Layout:
//
//	/var/lib/azcontainer/
//	├── images/
//	│   ├── <name>/                  ← per-image directory
//	│   │   ├── manifest.json        ← OCI manifest
//	│   │   ├── config.json          ← OCI config
//	│   │   └── rootfs/              ← extract olunmuş layer-lərin birləşməsi
//	│   └── alpine/                  ← köhnə layout (geriyə uyğunluq)
//	│       └── (rootfs birbaşa)
//	└── blobs/
//	    └── sha256/
//	        ├── <hash1>              ← layer tar.gz
//	        ├── <hash2>              ← config json
//	        └── ...
//
// Blob-lar content-addressable saxlanılır: bir layer bir neçə image-də
// istifadə olunsa belə, disk-də bir dəfə yer tutur.
package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// BaseDir — bütün image data-sının saxlandığı kök qovluq.
	BaseDir = "/var/lib/azcontainer"

	// ImagesDir — image-lər per-name burada saxlanılır.
	ImagesDir = BaseDir + "/images"

	// BlobsDir — content-addressable blob storage.
	BlobsDir = BaseDir + "/blobs/sha256"
)

// Store — lokal image storage idarəsi.
type Store struct {
	baseDir   string
	imagesDir string
	blobsDir  string
}

// NewStore — default path-lərlə yeni store yaradır.
//
// Lazımi qovluqları yaradır (yoxdursa).
func NewStore() (*Store, error) {
	s := &Store{
		baseDir:   BaseDir,
		imagesDir: ImagesDir,
		blobsDir:  BlobsDir,
	}

	for _, dir := range []string{s.imagesDir, s.blobsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("store directory yarat (%s): %w", dir, err)
		}
	}

	return s, nil
}

// BlobPath — verilmiş digest üçün lokal fayl path-i qaytarır.
//
// digest: "sha256:abc123..." format-ında
// Qaytarır: "/var/lib/azcontainer/blobs/sha256/abc123..."
func (s *Store) BlobPath(digest string) string {
	hash := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(s.blobsDir, hash)
}

// HasBlob — verilmiş digest üçün blob lokal olaraq mövcuddur?
func (s *Store) HasBlob(digest string) bool {
	_, err := os.Stat(s.BlobPath(digest))
	return err == nil
}

// WriteBlob — verilmiş reader-dən blob yazır və SHA256-nı yoxlayır.
//
// expectedDigest verilibsə ("sha256:..." və ya boş), hash uyğun gəlməsə xəta.
// Boş verilirsə, hash hesablanır və qaytarılır.
//
// Yazma prosesi:
//  1. Müvəqqəti fayla yaz (atomik olsun deyə)
//  2. Eyni anda SHA256 hesabla
//  3. Hash uyğundursa rename et (final path)
//  4. Uyğun deyilsə sil və xəta qaytar
func (s *Store) WriteBlob(r io.Reader, expectedDigest string) (string, error) {
	tmpFile, err := os.CreateTemp(s.blobsDir, ".blob-*")
	if err != nil {
		return "", fmt.Errorf("temp blob: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Hash hesablayan və eyni anda yazan teer.
	hasher := sha256.New()
	mw := io.MultiWriter(tmpFile, hasher)

	if _, err := io.Copy(mw, r); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("blob yaz: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("temp blob close: %w", err)
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	gotDigest := "sha256:" + gotHash

	// Expected verilibsə yoxla.
	if expectedDigest != "" && expectedDigest != gotDigest {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("digest uyğunsuzluğu: gözlənilən %s, alındı %s",
			expectedDigest, gotDigest)
	}

	// Final yerinə köçür.
	finalPath := filepath.Join(s.blobsDir, gotHash)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("blob rename: %w", err)
	}

	return gotDigest, nil
}

// SaveImage — manifest və config-i image-ə bağlayıb diskə yazır.
//
// /var/lib/azcontainer/images/<name>/manifest.json
// /var/lib/azcontainer/images/<name>/config.json
//
// Blob-lar artıq blobsDir-də olmalıdır (WriteBlob-la yazılmış).
func (s *Store) SaveImage(name string, manifest *Manifest, config *Config) error {
	imageDir := filepath.Join(s.imagesDir, name)
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return fmt.Errorf("image dir: %w", err)
	}

	if err := writeJSON(filepath.Join(imageDir, "manifest.json"), manifest); err != nil {
		return fmt.Errorf("manifest yaz: %w", err)
	}
	if err := writeJSON(filepath.Join(imageDir, "config.json"), config); err != nil {
		return fmt.Errorf("config yaz: %w", err)
	}

	return nil
}

// LoadImage — adına görə image-i yükləyir.
//
// Manifest və config-i oxuyur, lazımi blob-ların mövcudluğunu yoxlayır.
func (s *Store) LoadImage(name string) (*Manifest, *Config, error) {
	imageDir := filepath.Join(s.imagesDir, name)

	manifest := &Manifest{}
	if err := readJSON(filepath.Join(imageDir, "manifest.json"), manifest); err != nil {
		return nil, nil, fmt.Errorf("manifest oxu: %w", err)
	}

	config := &Config{}
	if err := readJSON(filepath.Join(imageDir, "config.json"), config); err != nil {
		return nil, nil, fmt.Errorf("config oxu: %w", err)
	}

	// Layer-lərin mövcudluğunu yoxla.
	for i, layer := range manifest.Layers {
		if !s.HasBlob(layer.Digest) {
			return nil, nil, fmt.Errorf("layer %d (%s) blob-da yoxdur", i, layer.Digest)
		}
	}

	return manifest, config, nil
}

// ListImages — bütün lokal image-lərin adlarını qaytarır.
func (s *Store) ListImages() ([]string, error) {
	entries, err := os.ReadDir(s.imagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("images dir oxu: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// manifest.json varsa, bu image-dir.
		manifestPath := filepath.Join(s.imagesDir, entry.Name(), "manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			names = append(names, entry.Name())
		}
	}

	return names, nil
}

// ImageRootFS — verilmiş image üçün assembled rootfs path-i qaytarır.
//
// Bu, OverlayFS-də lower kimi istifadə olunan qovluqdur.
func (s *Store) ImageRootFS(name string) string {
	return filepath.Join(s.imagesDir, name, "rootfs")
}

// — yardımçılar —

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
