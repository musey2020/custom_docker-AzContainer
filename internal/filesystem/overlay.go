// Package filesystem — container filesystem setup (OverlayFS + pivot_root).
package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LayoutPaths — bir container üçün OverlayFS directory layout-u.
//
// Hər container üçün belə struktur olur:
//
//	/var/lib/azcontainer/containers/<id>/
//	├── upper/      ← container-in yazdığı dəyişikliklər (per-container)
//	├── work/       ← OverlayFS internal scratch (per-container)
//	└── merged/     ← container bunu / kimi görür (mount point)
//
// Lower (Alpine rootfs) bütün container-lər arasında paylaşılır:
//
//	/var/lib/azcontainer/images/alpine/
type LayoutPaths struct {
	Lower  string // /var/lib/azcontainer/images/alpine
	Upper  string // /var/lib/azcontainer/containers/<id>/upper
	Work   string // /var/lib/azcontainer/containers/<id>/work
	Merged string // /var/lib/azcontainer/containers/<id>/merged
}

// NewLayoutForImage — image rootfs-i lower kimi istifadə edən layout yaradır.
//
// Mərhələ 5: image store assembled rootfs-i istifadə edirik.
// rootfsPath = /var/lib/azcontainer/images/<name>/rootfs
func NewLayoutForImage(containerID, rootfsPath string) (*LayoutPaths, error) {
	const containersDir = "/var/lib/azcontainer/containers"

	layout := &LayoutPaths{
		Lower:  rootfsPath,
		Upper:  filepath.Join(containersDir, containerID, "upper"),
		Work:   filepath.Join(containersDir, containerID, "work"),
		Merged: filepath.Join(containersDir, containerID, "merged"),
	}

	if _, err := os.Stat(layout.Lower); os.IsNotExist(err) {
		return nil, fmt.Errorf("rootfs tapılmadı: %s", layout.Lower)
	}

	for _, dir := range []string{layout.Upper, layout.Work, layout.Merged} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("directory %s: %w", dir, err)
		}
	}

	return layout, nil
}

// NewLayout — köhnə üsul (geriyə uyğunluq üçün).
//
// containerID — bu container üçün unikal ad (məsələn, "abc123")
// imageName — istifadə olunan image adı (məsələn, "alpine")
//
// Funksiya lazımi directory-ləri yaradır, amma mount etmir.
// Mount üçün ayrıca MountOverlay() çağırılır.
func NewLayout(containerID, imageName string) (*LayoutPaths, error) {
	const (
		imagesDir     = "/var/lib/azcontainer/images"
		containersDir = "/var/lib/azcontainer/containers"
	)

	layout := &LayoutPaths{
		Lower:  filepath.Join(imagesDir, imageName),
		Upper:  filepath.Join(containersDir, containerID, "upper"),
		Work:   filepath.Join(containersDir, containerID, "work"),
		Merged: filepath.Join(containersDir, containerID, "merged"),
	}

	// Lower (image) var olduğunu yoxla — yoxdursa, image yüklənməyib.
	if _, err := os.Stat(layout.Lower); os.IsNotExist(err) {
		return nil, fmt.Errorf("image %q tapılmadı: %s", imageName, layout.Lower)
	}

	// Container directory-lərini yarat.
	// MkdirAll lazım olan bütün ata-baba qovluqları da yaradır.
	for _, dir := range []string{layout.Upper, layout.Work, layout.Merged} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("directory yaratma %s: %w", dir, err)
		}
	}

	return layout, nil
}

// MountOverlay — OverlayFS-i merged directory-yə mount edir.
//
// Bu funksiya host-da çağırılır (Init-də yox, Run-da).
// Niyə? Çünki mount namespace child-da olsa da, OverlayFS-in özünü
// host-da qurmaq daha sadədir — child-a artıq hazır mount-u veririk.
//
// Mount syntax-ı:
//
//	mount -t overlay overlay -o lowerdir=X,upperdir=Y,workdir=Z /merged
func MountOverlay(layout *LayoutPaths) error {
	// OverlayFS options-u format et.
	// lowerdir = read-only base (Alpine)
	// upperdir = read-write layer (container-in dəyişiklikləri)
	// workdir  = OverlayFS internal scratch (boş olmalıdır, mount-dan əvvəl)
	options := fmt.Sprintf(
		"lowerdir=%s,upperdir=%s,workdir=%s",
		layout.Lower,
		layout.Upper,
		layout.Work,
	)

	// syscall.Mount parametrləri:
	//   source     — "overlay" (overlay-də əhəmiyyəti yoxdur, amma tələb edilir)
	//   target     — merged directory
	//   filesystem — "overlay"
	//   flags      — 0 (xüsusi flag yoxdur)
	//   data       — yuxarıdakı options string
	if err := syscall.Mount("overlay", layout.Merged, "overlay", 0, options); err != nil {
		return fmt.Errorf("OverlayFS mount: %w", err)
	}

	return nil
}

// UnmountOverlay — container bitəndə OverlayFS-i unmount edir.
//
// MNT_DETACH istifadə edirik — bu "lazy unmount"-dur, yəni mount nöqtəsi
// dərhal silinir, amma daxili istinadlar varsa onlar bitənə qədər saxlanılır.
// Bu, container hələ tam çıxmamışsa "device busy" error-ından qoruyur.
func UnmountOverlay(layout *LayoutPaths) error {
	if err := syscall.Unmount(layout.Merged, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("OverlayFS unmount: %w", err)
	}
	return nil
}

// Cleanup — container directory-sini tamamilə silir.
//
// Diqqət: UnmountOverlay-dən SONRA çağırılmalıdır.
// Əgər mount hələ aktivdirsə, bu funksiya host-un faylllarını silə bilər!
func Cleanup(layout *LayoutPaths) error {
	// Container ID-ni containers/<id>/ səviyyəsindən tap.
	// layout.Upper = /var/lib/azcontainer/containers/<id>/upper
	// Bizə /var/lib/azcontainer/containers/<id>/ lazımdır.
	containerDir := filepath.Dir(layout.Upper)

	if err := os.RemoveAll(containerDir); err != nil {
		return fmt.Errorf("container directory silmə: %w", err)
	}
	return nil
}
