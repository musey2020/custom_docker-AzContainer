package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// PivotRoot — proses-in root directory-sini newRoot-a dəyişdirir.
//
// Bu funksiya container daxilində (Init-də) çağırılır.
// Funksiya çağrıldıqdan sonra:
//   - Proses-in /-i artıq newRoot-dur
//   - Köhnə host filesystem tamamilə kəsilib
//   - /proc, /sys, /dev hələ mount olunmayıb (ayrı funksiyada)
//
// newRoot — yeni root olacaq directory (məsələn, OverlayFS merged path)
func PivotRoot(newRoot string) error {
	// ADDIM 1: Mount propagation-u "private" et.
	//
	// Default-da Linux mount-ları "shared" propagation-da ola bilər.
	// Yəni biz container-də mount edəndə host da görür.
	// `make-rprivate /` bütün mount-ları private edir — host artıq təsirlənmir.
	//
	// Bu addım pivot_root üçün KRİTİKDİR.
	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("mount propagation private: %w", err)
	}

	// ADDIM 2: newRoot-u özünə bind mount et.
	//
	// pivot_root tələb edir ki, newRoot ayrıca mount olsun (sadəcə directory yox).
	// Həll: directory-ni özünə bind mount et — kernel ona "mount" kimi baxır.
	if err := syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("rootfs bind mount: %w", err)
	}

	// ADDIM 3: Köhnə root üçün directory yarat.
	//
	// put_old newRoot-un İÇİNDƏ olmalıdır. Standart yer: newRoot/.pivot_old
	putOld := filepath.Join(newRoot, ".pivot_old")
	if err := os.MkdirAll(putOld, 0700); err != nil {
		return fmt.Errorf(".pivot_old yarat: %w", err)
	}

	// ADDIM 4: pivot_root çağır.
	//
	// Kernel-ə deyirik:
	//   "Yeni root newRoot-dur. Köhnə root-u putOld-ə qoy."
	if err := syscall.PivotRoot(newRoot, putOld); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	// ADDIM 5: Current working directory-ni yenilə.
	//
	// pivot_root-dan sonra cwd köhnə root-u göstərə bilər. / -ə qayıt.
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// ADDIM 6: Köhnə root-u unmount et.
	//
	// İndi proses-in nöqteyi-nəzərindən köhnə root /.pivot_old-da-dır.
	// Onu unmount edirik — host filesystem-i tamamilə kəsilir.
	//
	// MNT_DETACH = lazy unmount (təhlükəsizdir).
	if err := syscall.Unmount("/.pivot_old", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("köhnə root unmount: %w", err)
	}

	// ADDIM 7: Boş .pivot_old qovluğunu sil.
	if err := os.Remove("/.pivot_old"); err != nil {
		return fmt.Errorf(".pivot_old sil: %w", err)
	}

	return nil
}

// MountVirtualFilesystems — container daxilində /proc, /sys, /dev mount edir.
//
// Bu funksiya PivotRoot-dan SONRA çağırılır.
//
// /proc — procfs, container-in proses-lərini göstərir (PID namespace sayəsində)
// /sys  — sysfs, kernel/device məlumatı
// /dev  — tmpfs, sadə device file-lar üçün (Mərhələ 4-də genişləndirəcəyik)
func MountVirtualFilesystems() error {
	// /proc mount — container daxilində ps aux düzgün işləməsi üçün KRİTİKDİR.
	// nodev, noexec, nosuid — təhlükəsizlik flag-ləri.
	if err := syscall.Mount("proc", "/proc", "proc",
		syscall.MS_NOEXEC|syscall.MS_NOSUID|syscall.MS_NODEV, ""); err != nil {
		return fmt.Errorf("/proc mount: %w", err)
	}

	// /sys mount — sysfs.
	// MS_RDONLY əlavə edirik, container kernel parametrlərini dəyişməsin.
	if err := syscall.Mount("sysfs", "/sys", "sysfs",
		syscall.MS_NOEXEC|syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_RDONLY, ""); err != nil {
		return fmt.Errorf("/sys mount: %w", err)
	}

	// /dev mount — tmpfs.
	// Real device-lər yox, sadəcə minimal mühit.
	if err := syscall.Mount("tmpfs", "/dev", "tmpfs",
		syscall.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755"); err != nil {
		return fmt.Errorf("/dev mount: %w", err)
	}

	return nil
}
