// Package image — Docker registry HTTP client.
//
// Docker Hub və ya digər OCI-uyğun registry-dən image çəkir.
//
// Protokol:
//  1. GET /v2/                                      → auth challenge alırıq
//  2. GET auth.docker.io/token                      → bearer token al
//  3. GET /v2/<image>/manifests/<tag>               → manifest
//  4. GET /v2/<image>/blobs/<config_digest>         → config
//  5. GET /v2/<image>/blobs/<layer_digest>          → hər layer üçün
package image

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultRegistry = "registry-1.docker.io"
	authService     = "auth.docker.io"
)

// Puller — registry-dən image pull edir.
type Puller struct {
	store      *Store
	httpClient *http.Client
	registry   string
}

func NewPuller(s *Store) *Puller {
	return &Puller{
		store:    s,
		registry: defaultRegistry,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Pull — verilmiş image-i registry-dən endirib lokal saxlayır.
//
// imageRef formatları:
//
//	"alpine"            → library/alpine:latest
//	"alpine:3.20"       → library/alpine:3.20
//	"nginx:stable"      → library/nginx:stable
func (p *Puller) Pull(imageRef, localName string) error {
	repo, tag := parseImageRef(imageRef)
	fmt.Printf("Pulling %s:%s\n", repo, tag)

	// ADDIM 1: Token al.
	token, err := p.getToken(repo)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	// ADDIM 2: Manifest al.
	manifest, err := p.fetchManifest(repo, tag, token)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	fmt.Printf("Manifest alındı (%d layer)\n", len(manifest.Layers))

	// ADDIM 3: Config blob.
	if !p.store.HasBlob(manifest.Config.Digest) {
		fmt.Printf("Config endirilir...\n")
		if err := p.fetchBlob(repo, manifest.Config.Digest, token); err != nil {
			return fmt.Errorf("config blob: %w", err)
		}
	}

	// Config-i parse et (saxlamaq üçün lazımdır).
	configPath := p.store.BlobPath(manifest.Config.Digest)
	configBytes, err := readFile(configPath)
	if err != nil {
		return fmt.Errorf("config oxu: %w", err)
	}
	config := &Config{}
	if err := json.Unmarshal(configBytes, config); err != nil {
		return fmt.Errorf("config parse: %w", err)
	}

	// ADDIM 4: Layer-ləri endir.
	for i, layer := range manifest.Layers {
		if p.store.HasBlob(layer.Digest) {
			fmt.Printf("Layer %d/%d: cache (%s)\n", i+1, len(manifest.Layers), shortDigest(layer.Digest))
			continue
		}
		fmt.Printf("Layer %d/%d: endirilir %s (%d MB)\n",
			i+1, len(manifest.Layers), shortDigest(layer.Digest), layer.Size/1024/1024)
		if err := p.fetchBlob(repo, layer.Digest, token); err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
	}

	// ADDIM 5: Image-i lokal saxla.
	if err := p.store.SaveImage(localName, manifest, config); err != nil {
		return fmt.Errorf("image saxla: %w", err)
	}

	fmt.Printf("✓ Image pull edildi: %s\n", localName)
	return nil
}

// getToken — registry üçün bearer token alır (anonymous).
func (p *Puller) getToken(repo string) (string, error) {
	url := fmt.Sprintf("https://%s/token?service=%s&scope=repository:%s:pull",
		authService, p.registry, repo)

	resp, err := p.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token HTTP %d", resp.StatusCode)
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	return tokenResp.Token, nil
}

// fetchManifest — image manifest-ini alır.
func (p *Puller) fetchManifest(repo, tag, token string) (*Manifest, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", p.registry, repo, tag)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// Hər iki format-ı qəbul edirik.
	req.Header.Set("Accept",
		"application/vnd.docker.distribution.manifest.v2+json,"+
			"application/vnd.oci.image.manifest.v1+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest HTTP %d: %s", resp.StatusCode, string(body))
	}

	manifest := &Manifest{}
	if err := json.NewDecoder(resp.Body).Decode(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

// fetchBlob — blob endirib store-a yazır (hash yoxlaması ilə).
func (p *Puller) fetchBlob(repo, digest, token string) error {
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", p.registry, repo, digest)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("blob HTTP %d", resp.StatusCode)
	}

	// WriteBlob digest-i yoxlayır.
	_, err = p.store.WriteBlob(resp.Body, digest)
	return err
}

// parseImageRef — "alpine:3.20" → ("library/alpine", "3.20")
func parseImageRef(ref string) (repo, tag string) {
	parts := strings.SplitN(ref, ":", 2)
	repo = parts[0]
	tag = "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}
	// Docker Hub-da rəsmi image-lərə "library/" prefiks lazımdır.
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return repo, tag
}

func shortDigest(d string) string {
	d = strings.TrimPrefix(d, "sha256:")
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
