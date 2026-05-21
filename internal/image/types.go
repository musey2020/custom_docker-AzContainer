// Package image — OCI image format dəstəyi.
//
// Mərhələ 5a: Tipləri və store layout-u.
//
// OCI Image Spec v1.0:
//   - Image bir neçə layer-dən ibarətdir (tar.gz fayllar)
//   - Manifest layer-lərə və config-ə işarə edir (SHA256 hash ilə)
//   - Config CMD, ENV, WorkingDir və s. saxlayır
//   - Hər şey content-addressable: ad yox, hash var
package image

// Descriptor — OCI-də blob-a (binary obyektə) istinad.
//
// MediaType + Digest + Size ilə blob unikal identifikasiya olunur.
// Spec: https://github.com/opencontainers/image-spec/blob/main/descriptor.md
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"` // "sha256:abc123..."
	Size      int64  `json:"size"`
}

// Manifest — bir image-i təşkil edən komponentləri sadalayır.
//
// Bir image üçün bir manifest var. Manifest:
//   - Config blob-una işarə edir (image-in metadata-sı)
//   - Layer blob-larına işarə edir (faktiki fayl sistemi)
//
// MediaType nümunələri:
//   - "application/vnd.oci.image.manifest.v1+json"          (OCI standart)
//   - "application/vnd.docker.distribution.manifest.v2+json" (Docker)
type Manifest struct {
	SchemaVersion int          `json:"schemaVersion"` // həmişə 2
	MediaType     string       `json:"mediaType,omitempty"`
	Config        Descriptor   `json:"config"`
	Layers        []Descriptor `json:"layers"`
}

// Config — image-in runtime metadata-sı.
//
// `docker inspect` bu məlumatları göstərir.
// Container start olduqda biz bu məlumatdan istifadə edirik:
//   - Cmd → istifadəçi komanda verməyibsə default
//   - Env → environment dəyişənləri
//   - WorkingDir → cwd
type Config struct {
	Architecture string      `json:"architecture"`      // "amd64", "arm64"
	OS           string      `json:"os"`                // "linux"
	Config       ImageConfig `json:"config"`            // runtime parametrlər
	RootFS       RootFS      `json:"rootfs"`            // layer DiffIDs
	History      []History   `json:"history,omitempty"` // build addımları (opsional)
}

// ImageConfig — container start olduqda istifadə olunan parametrlər.
type ImageConfig struct {
	User       string            `json:"User,omitempty"`       // "1000:1000" və ya "root"
	Env        []string          `json:"Env,omitempty"`        // ["PATH=...", "HOME=/root"]
	Entrypoint []string          `json:"Entrypoint,omitempty"` // dəyişməz başlanğıc
	Cmd        []string          `json:"Cmd,omitempty"`        // override edilə bilən
	WorkingDir string            `json:"WorkingDir,omitempty"` // cwd
	Labels     map[string]string `json:"Labels,omitempty"`     // metadata
}

// RootFS — layer-lərin DiffID-lərini sadalayır.
//
// DiffID = uncompressed layer-in SHA256-sı.
// Digest (manifest-də) = compressed layer-in SHA256-sı.
// Bunlar fərqli olduğu üçün ayrı saxlanılır.
type RootFS struct {
	Type    string   `json:"type"`     // həmişə "layers"
	DiffIDs []string `json:"diff_ids"` // ["sha256:...", "sha256:..."]
}

// History — build addımları (bizim üçün məcburi deyil, amma parse edirik).
type History struct {
	Created    string `json:"created,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
}

// dockerManifest — `docker save` format-ı.
//
// Docker save adi OCI image-dən fərqli format istifadə edir:
//   - manifest.json (kök, plural manifests)
//   - <id>.json (config faylı)
//   - <id>/layer.tar (layer-lər, sıxılmamış)
//
// Biz hər iki format-ı dəstəkləyəcəyik — bu, docker save formatı üçün.
type dockerManifest struct {
	Config   string   `json:"Config"`   // "<hash>.json"
	RepoTags []string `json:"RepoTags"` // ["alpine:latest"]
	Layers   []string `json:"Layers"`   // ["<hash>/layer.tar", ...]
}
