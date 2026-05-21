// Package security — container security mexanizmləri.
//
// Mərhələ 4a + 4b:
// - no_new_privs (4a)
// - capabilities drop (4b)
//
// Sonra (Mərhələ 4c) əlavə olunacaq:
// - Seccomp filter
package security

import (
	"fmt"
	"syscall"
)

// PR_SET_NO_NEW_PRIVS — prctl(2) operation kodu.
//
// Bu sabit Linux kernel-də müəyyən edilib (linux/prctl.h).
// Dəyəri 38-dir və heç vaxt dəyişməz (ABI stabilliyi).
const PR_SET_NO_NEW_PRIVS = 38

// SetNoNewPrivs — proses üçün no_new_privs flag-ini aktivləşdirir.
//
// Bu çağırışdan sonra:
//   - setuid binary-lər ignore olunur (root kimi açılmır)
//   - setgid binary-lər ignore olunur
//   - file capabilities ignore olunur
//
// Yəni proses HEÇ VAXT mövcud privilege-dən çoxuna sahib ola bilməz.
// Bu flag bir dəfə set edildikdən sonra GERİ ALINMIR — bu, qəsdən belədir.
//
// Bu, exec-dən ƏVVƏL çağırılmalıdır.
//
// VACIB: capabilities drop-dan ƏVVƏL çağırılmalıdır, çünki
// no_new_privs aktivdirsə, exec-dən sonra capability-lər qazanmaq mümkün olmur.
func SetNoNewPrivs() error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_PRCTL,
		PR_SET_NO_NEW_PRIVS,
		1,
		0, 0, 0, 0,
	)

	if errno != 0 {
		return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS): %w", errno)
	}
	return nil
}

// Apply — bütün security mexanizmlərini tətbiq edir.
//
// Sıralama VACIBDIR:
//  1. no_new_privs — exec-dən sonra privilege qazanmağı bloklayır
//  2. capabilities drop — bounding set-i məhdudlaşdırır
//
// Hər ikisi exec-dən əvvəl, Init() funksiyasında çağırılır.
func Apply() error {
	// 1. no_new_privs — əvvəlcə bunu set et ki, sonrakı addımlar
	// qorunsun (artıq privilege qazanmaq mümkün olmasın).
	if err := SetNoNewPrivs(); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}

	// 2. Capabilities drop — bounding set-dən təhlükəli capability-ləri at.
	if err := DropCapabilities(); err != nil {
		return fmt.Errorf("capabilities drop: %w", err)
	}

	// 3. Seccomp filter — təhlükəli syscall-ları blokla.
	// VACIBDIR: SON addımdır, çünki seccomp özü prctl-ı blokla bilər.
	if err := LoadSeccompFilter(); err != nil {
		return fmt.Errorf("seccomp: %w", err)
	}

	return nil
}
