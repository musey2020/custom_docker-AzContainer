// Package security — capabilities drop.
//
// Mərhələ 4b: Linux capabilities-i məhdudlaşdırırıq.
//
// İdeya: container daxilindəki proses root (uid=0) olsa belə,
// "tam root" deyil — yalnız bizim icazə verdiyimiz capability-lərə
// sahibdir. Qalanları "bounding set"-dən atırıq və artıq HEÇ VAXT
// geri qaytarıla bilməz.
package security

import (
	"fmt"
	"syscall"
)

// prctl operation kodları — kernel-də sabitdir (linux/prctl.h).
const (
	PR_CAPBSET_READ = 23 // bounding set-də capability var?
	PR_CAPBSET_DROP = 24 // bounding set-dən capability at
)

// Capability nömrələri — kernel-də sabitdir (linux/capability.h).
//
// Bu nömrələr ABI-stabil-dir, heç vaxt dəyişmir.
// Yalnız yeni capability-lər siyahıya əlavə olunur (sona).
const (
	CAP_CHOWN              = 0
	CAP_DAC_OVERRIDE       = 1
	CAP_DAC_READ_SEARCH    = 2
	CAP_FOWNER             = 3
	CAP_FSETID             = 4
	CAP_KILL               = 5
	CAP_SETGID             = 6
	CAP_SETUID             = 7
	CAP_SETPCAP            = 8
	CAP_LINUX_IMMUTABLE    = 9
	CAP_NET_BIND_SERVICE   = 10
	CAP_NET_BROADCAST      = 11
	CAP_NET_ADMIN          = 12
	CAP_NET_RAW            = 13
	CAP_IPC_LOCK           = 14
	CAP_IPC_OWNER          = 15
	CAP_SYS_MODULE         = 16
	CAP_SYS_RAWIO          = 17
	CAP_SYS_CHROOT         = 18
	CAP_SYS_PTRACE         = 19
	CAP_SYS_PACCT          = 20
	CAP_SYS_ADMIN          = 21
	CAP_SYS_BOOT           = 22
	CAP_SYS_NICE           = 23
	CAP_SYS_RESOURCE       = 24
	CAP_SYS_TIME           = 25
	CAP_SYS_TTY_CONFIG     = 26
	CAP_MKNOD              = 27
	CAP_LEASE              = 28
	CAP_AUDIT_WRITE        = 29
	CAP_AUDIT_CONTROL      = 30
	CAP_SETFCAP            = 31
	CAP_MAC_OVERRIDE       = 32
	CAP_MAC_ADMIN          = 33
	CAP_SYSLOG             = 34
	CAP_WAKE_ALARM         = 35
	CAP_BLOCK_SUSPEND      = 36
	CAP_AUDIT_READ         = 37
	CAP_PERFMON            = 38
	CAP_BPF                = 39
	CAP_CHECKPOINT_RESTORE = 40
)

// capabilityName — log üçün capability-nin oxunaqlı adı.
//
// Map-i package-level saxlamırıq, çünki yalnız drop log-unda lazımdır.
// Hər çağırışda yenidən qurulması ucuzdur (40+ element).
func capabilityName(cap uintptr) string {
	names := map[uintptr]string{
		CAP_CHOWN:              "CAP_CHOWN",
		CAP_DAC_OVERRIDE:       "CAP_DAC_OVERRIDE",
		CAP_DAC_READ_SEARCH:    "CAP_DAC_READ_SEARCH",
		CAP_FOWNER:             "CAP_FOWNER",
		CAP_FSETID:             "CAP_FSETID",
		CAP_KILL:               "CAP_KILL",
		CAP_SETGID:             "CAP_SETGID",
		CAP_SETUID:             "CAP_SETUID",
		CAP_SETPCAP:            "CAP_SETPCAP",
		CAP_LINUX_IMMUTABLE:    "CAP_LINUX_IMMUTABLE",
		CAP_NET_BIND_SERVICE:   "CAP_NET_BIND_SERVICE",
		CAP_NET_BROADCAST:      "CAP_NET_BROADCAST",
		CAP_NET_ADMIN:          "CAP_NET_ADMIN",
		CAP_NET_RAW:            "CAP_NET_RAW",
		CAP_IPC_LOCK:           "CAP_IPC_LOCK",
		CAP_IPC_OWNER:          "CAP_IPC_OWNER",
		CAP_SYS_MODULE:         "CAP_SYS_MODULE",
		CAP_SYS_RAWIO:          "CAP_SYS_RAWIO",
		CAP_SYS_CHROOT:         "CAP_SYS_CHROOT",
		CAP_SYS_PTRACE:         "CAP_SYS_PTRACE",
		CAP_SYS_PACCT:          "CAP_SYS_PACCT",
		CAP_SYS_ADMIN:          "CAP_SYS_ADMIN",
		CAP_SYS_BOOT:           "CAP_SYS_BOOT",
		CAP_SYS_NICE:           "CAP_SYS_NICE",
		CAP_SYS_RESOURCE:       "CAP_SYS_RESOURCE",
		CAP_SYS_TIME:           "CAP_SYS_TIME",
		CAP_SYS_TTY_CONFIG:     "CAP_SYS_TTY_CONFIG",
		CAP_MKNOD:              "CAP_MKNOD",
		CAP_LEASE:              "CAP_LEASE",
		CAP_AUDIT_WRITE:        "CAP_AUDIT_WRITE",
		CAP_AUDIT_CONTROL:      "CAP_AUDIT_CONTROL",
		CAP_SETFCAP:            "CAP_SETFCAP",
		CAP_MAC_OVERRIDE:       "CAP_MAC_OVERRIDE",
		CAP_MAC_ADMIN:          "CAP_MAC_ADMIN",
		CAP_SYSLOG:             "CAP_SYSLOG",
		CAP_WAKE_ALARM:         "CAP_WAKE_ALARM",
		CAP_BLOCK_SUSPEND:      "CAP_BLOCK_SUSPEND",
		CAP_AUDIT_READ:         "CAP_AUDIT_READ",
		CAP_PERFMON:            "CAP_PERFMON",
		CAP_BPF:                "CAP_BPF",
		CAP_CHECKPOINT_RESTORE: "CAP_CHECKPOINT_RESTORE",
	}
	if name, ok := names[cap]; ok {
		return name
	}
	return fmt.Sprintf("CAP_UNKNOWN_%d", cap)
}

// allowedCapabilities — container daxilində SAXLANILAN capability-lər.
//
// Bu siyahı Docker default-una yaxındır, amma şəbəkə hələ yox olduğu üçün
// CAP_NET_RAW və CAP_NET_BIND_SERVICE-i də siyahıdan çıxarmaq olar.
// İndi CAP_NET_BIND_SERVICE-i saxlayırıq, çünki gələcəkdə (Mərhələ 7)
// şəbəkə əlavə olunanda lazım olacaq.
//
// Siyahıda OLMAYAN hər şey drop edilir.
var allowedCapabilities = map[uintptr]bool{
	CAP_CHOWN:            true, // fayl sahibini dəyişmək (apk install)
	CAP_DAC_OVERRIDE:     true, // root-un fayl icazələrini ignore etməsi
	CAP_FOWNER:           true, // sahibi olmayan fayllarda chmod
	CAP_FSETID:           true, // setuid/setgid bit qoruma
	CAP_KILL:             true, // signal göndərmək
	CAP_SETGID:           true, // gid dəyişmək (su, login)
	CAP_SETUID:           true, // uid dəyişmək (su, login)
	CAP_SETPCAP:          true, // capability-ləri öz daxilində manipulyasiya
	CAP_NET_BIND_SERVICE: true, // port <1024 (gələcək şəbəkə üçün)
	CAP_SYS_CHROOT:       true, // chroot syscall
	CAP_MKNOD:            true, // device file yaratmaq (məhdud)
	CAP_AUDIT_WRITE:      true, // login işləməsi üçün
	CAP_SETFCAP:          true, // file capabilities qoymaq
}

// maxCapability — bilinən ən böyük capability nömrəsi.
//
// Kernel yeni capability-lər əlavə edə bilər. Bu rəqəm ən azı yuxarıda
// elan edilənlərdən böyük olmalıdır. 64 verir hətta gələcək capability-lər
// üçün də yer (kernel-də praktikada hələ ~41-dədir).
const maxCapability = 64

// DropCapabilities — bounding set-dən bütün lazımsız capability-ləri atır.
//
// Bu funksiya exec-dən ƏVVƏL çağırılmalıdır (Init-də).
//
// İş prinsipi:
//  1. 0-dan maxCapability-yə qədər hər capability üçün:
//     a. PR_CAPBSET_READ ilə yoxla — kernel bu capability-ni tanıyır?
//     b. Tanıyırsa və allowedCapabilities-də YOXDURSA → PR_CAPBSET_DROP
//  2. Drop edildikdən sonra capability HEÇ VAXT geri qayıtmır.
//
// no_new_privs ilə birlikdə bu, container-i privilege escalation-dan qoruyur.
func DropCapabilities() error {
	dropped := 0

	for cap := uintptr(0); cap < maxCapability; cap++ {
		// PR_CAPBSET_READ — kernel bu capability nömrəsini tanıyırmı?
		//
		// prctl(PR_CAPBSET_READ, cap, 0, 0, 0):
		//   qaytarır 1 → bounding set-də var
		//   qaytarır 0 → bounding set-də yoxdur (artıq drop edilib)
		//   qaytarır -1 (errno=EINVAL) → kernel bu nömrəni tanımır
		ret, _, errno := syscall.Syscall6(
			syscall.SYS_PRCTL,
			PR_CAPBSET_READ,
			cap,
			0, 0, 0, 0,
		)

		if errno != 0 {
			// EINVAL → bu capability nömrəsi mövcud deyil.
			// Bu xəta deyil, sadəcə kernel bu rəqəmi tanımır.
			// Davam edirik (yeni kernel-də başqa capability ola bilər).
			if errno == syscall.EINVAL {
				continue
			}
			return fmt.Errorf("PR_CAPBSET_READ(%d): %w", cap, errno)
		}

		// Bounding set-də yoxdursa, atmağa ehtiyac yox.
		if ret == 0 {
			continue
		}

		// Siyahıda varsa, saxlayırıq.
		if allowedCapabilities[cap] {
			continue
		}

		// Drop et.
		//
		// prctl(PR_CAPBSET_DROP, cap, 0, 0, 0)
		// Uğursuz olarsa, EPERM qayıdar (proses-in CAP_SETPCAP-ı yoxdur).
		// Amma biz root-uq və CAP_SETPCAP hələ atılmayıb, ona görə işləməlidir.
		_, _, errno = syscall.Syscall6(
			syscall.SYS_PRCTL,
			PR_CAPBSET_DROP,
			cap,
			0, 0, 0, 0,
		)

		if errno != 0 {
			return fmt.Errorf("PR_CAPBSET_DROP(%s): %w", capabilityName(cap), errno)
		}

		dropped++
	}

	fmt.Printf("[container] %d capability drop edildi\n", dropped)
	return nil
}
