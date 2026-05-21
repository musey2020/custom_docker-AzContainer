// Package security — seccomp filter.
//
// Mərhələ 4c: Linux syscall-larını məhdudlaşdırırıq.
//
// Yanaşma: Docker default-u kimi blacklist.
// Təhlükəli syscall-lar ENOSYS qaytarır, qalanları işləyir.
//
// BPF bytecode-u əl ilə qururuq — libseccomp yoxdur, CGO yoxdur.
package security

import (
	"fmt"
	"syscall"
	"unsafe"
)

// BPF instruction format-ı kernel-də belə təyin edilib (linux/filter.h):
//
//	struct sock_filter {
//	    __u16 code;   // əməliyyat kodu
//	    __u8  jt;     // şərt doğrudursa: bu qədər instruction-u atla
//	    __u8  jf;     // şərt yalandırsa: bu qədər instruction-u atla
//	    __u32 k;      // əməliyyat üçün argument
//	};
type sockFilter struct {
	Code uint16
	JT   uint8
	JF   uint8
	K    uint32
}

// sock_fprog — bütün filter proqramını təmsil edir.
type sockFprog struct {
	Len    uint16
	_      [6]byte // padding (8-byte alignment for filter pointer)
	Filter *sockFilter
}

// BPF instruction kodları (linux/bpf_common.h).
const (
	BPF_LD  = 0x00 // load
	BPF_W   = 0x00 // word (32-bit)
	BPF_ABS = 0x20 // absolute offset
	BPF_JMP = 0x05 // jump
	BPF_JEQ = 0x10 // equal
	BPF_K   = 0x00 // immediate value
	BPF_RET = 0x06 // return
)

// Seccomp qərarları (linux/seccomp.h).
const (
	SECCOMP_RET_KILL_PROCESS = 0x80000000
	SECCOMP_RET_ERRNO        = 0x00050000
	SECCOMP_RET_ALLOW        = 0x7fff0000
)

// prctl operation-ları seccomp üçün.
const (
	PR_SET_SECCOMP          = 22
	SECCOMP_MODE_FILTER     = 2
	SECCOMP_SET_MODE_FILTER = 1
)

// seccomp_data struct-ında syscall nömrəsinin offset-i.
// struct: nr (4 byte) | arch (4 byte) | ip (8) | args[6] (48)
const (
	offsetNR   = 0
	offsetArch = 4
)

// AUDIT_ARCH_X86_64 — x86_64 arxitektura identifikatoru.
// linux/audit.h-dən. Başqa arxitekturalar üçün dəyər fərqlidir.
const AUDIT_ARCH_X86_64 = 0xC000003E

// blockedSyscalls — bloklanan syscall-lar (Docker default-una yaxın).
// Nömrələr x86_64 üçün stabil ABI-dir.
// Mənbə: /usr/include/x86_64-linux-gnu/asm/unistd_64.h
var blockedSyscalls = []uint32{
	// Mount/filesystem manipulyasiya
	165, // mount
	166, // umount2
	155, // pivot_root
	139, // sysfs

	// Sistem idarəsi
	169, // reboot
	164, // settimeofday  (DÜZƏLDİLDİ: əvvəl 176 idi, o delete_module-dur)
	227, // clock_settime
	159, // adjtimex
	308, // setns - namespace dəyişmək

	// Kernel module
	175, // init_module
	176, // delete_module
	313, // finit_module

	// Kexec
	246, // kexec_load
	320, // kexec_file_load

	// I/O port
	172, // iopl
	173, // ioperm

	// Async I/O (potensial DoS)
	206, // io_setup
	207, // io_destroy

	// Sistem
	103, // syslog
	179, // quotactl  (DÜZƏLDİLDİ: əvvəl 152 idi)

	// Keyring
	248, // add_key
	249, // request_key
	250, // keyctl

	// Proses inspeksiyası
	312, // kcmp
	298, // perf_event_open

	// File handle (open_by_handle_at root takeover üçün istifadə olunub)
	303, // name_to_handle_at
	304, // open_by_handle_at

	// BPF
	321, // bpf

	// Userfaultfd (CVE-2016-2545 və s.)
	323, // userfaultfd

	// Memory manipulyasiya
	256, // migrate_pages
	279, // move_pages
}

// buildFilter — BPF programını qurur.
//
// Struktur:
//  1. arch yoxlamasi: AUDIT_ARCH_X86_64 deyilsə → KILL
//  2. syscall nömrəsini yüklə
//  3. blokled siyahısının hər biri üçün: bərabərdirsə → ENOSYS
//  4. Default → ALLOW
func buildFilter() []sockFilter {
	filter := []sockFilter{
		// (1) arch yüklə: A = seccomp_data.arch
		{Code: BPF_LD | BPF_W | BPF_ABS, K: offsetArch},
		// arch == AUDIT_ARCH_X86_64? bərabərdirsə 1 atla, yox isə KILL-ə düş
		{Code: BPF_JMP | BPF_JEQ | BPF_K, JT: 1, JF: 0, K: AUDIT_ARCH_X86_64},
		// arch yanlışdır → proses-i öldür
		{Code: BPF_RET | BPF_K, K: SECCOMP_RET_KILL_PROCESS},
		// (2) syscall nömrəsini yüklə: A = seccomp_data.nr
		{Code: BPF_LD | BPF_W | BPF_ABS, K: offsetNR},
	}

	// (3) Hər bloklanan syscall üçün: bərabərdirsə → return ENOSYS, yox isə → davam
	for _, nr := range blockedSyscalls {
		filter = append(filter,
			// nr ilə bərabərdirsə → 0 atla (növbəti instruction), yox isə → 1 atla
			sockFilter{Code: BPF_JMP | BPF_JEQ | BPF_K, JT: 0, JF: 1, K: nr},
			// ENOSYS qaytar
			sockFilter{Code: BPF_RET | BPF_K, K: SECCOMP_RET_ERRNO | uint32(syscall.ENOSYS)},
		)
	}

	// (4) Default: ALLOW
	filter = append(filter, sockFilter{Code: BPF_RET | BPF_K, K: SECCOMP_RET_ALLOW})

	return filter
}

// LoadSeccompFilter — seccomp filter-i kernel-ə yükləyir.
//
// no_new_privs aktiv olmalıdır (Apply()-də artıq edilir).
// Bu çağırışdan sonra geri alınmır.
func LoadSeccompFilter() error {
	filter := buildFilter()

	prog := sockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}

	// prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog)
	_, _, errno := syscall.Syscall(
		syscall.SYS_PRCTL,
		PR_SET_SECCOMP,
		SECCOMP_MODE_FILTER,
		uintptr(unsafe.Pointer(&prog)),
	)

	if errno != 0 {
		return fmt.Errorf("prctl(PR_SET_SECCOMP): %w", errno)
	}

	fmt.Printf("[container] seccomp filter yükləndi (%d syscall bloklandı)\n", len(blockedSyscalls))
	return nil
}
