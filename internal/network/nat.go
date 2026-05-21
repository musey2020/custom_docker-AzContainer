// Package network — iptables NAT (MASQUERADE).
//
// Container-in private IP-si (10.88.x.x) internetə birbaşa çıxa bilməz.
// MASQUERADE qaydası ilə host kernel paketi öz public IP-si kimi göstərir.
//
// Bir də: IP forwarding aktiv olmalıdır (sysctl net.ipv4.ip_forward=1).
package network

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EnableIPForward — kernel-də IP forwarding aktivləşdirir.
//
// Bu lazımdır ki, host paketləri bir interface-dən digərinə ötürsün
// (bridge-dən eth0-a və əksinə).
func EnableIPForward() error {
	path := "/proc/sys/net/ipv4/ip_forward"
	return os.WriteFile(path, []byte("1"), 0644)
}

// EnsureNAT — bridge subnet üçün MASQUERADE qaydasını qurur.
//
// Komanda: iptables -t nat -A POSTROUTING -s 10.88.0.0/16 ! -o azcontainer0 -j MASQUERADE
//
// Mənası:
//
//	-t nat                       → NAT cədvəli
//	-A POSTROUTING               → paket çıxarkən
//	-s 10.88.0.0/16              → mənbə container subneti-dirsə
//	! -o azcontainer0            → çıxış interface bridge DEYİL-sə (yəni real internet)
//	-j MASQUERADE                → source NAT et (host-un IP-si ilə əvəz)
func EnsureNAT() error {
	// Köhnə eyni qayda varsa, ikinci dəfə əlavə etmə.
	if natRuleExists() {
		return nil
	}

	fmt.Println("[host] iptables MASQUERADE qaydası əlavə edilir")

	args := []string{
		"-t", "nat",
		"-A", "POSTROUTING",
		"-s", Subnet,
		"!", "-o", BridgeName,
		"-j", "MASQUERADE",
	}
	return runCmd("iptables", args...)
}

// natRuleExists — eyni qayda mövcuddurmu?
func natRuleExists() bool {
	args := []string{
		"-t", "nat",
		"-C", "POSTROUTING",
		"-s", Subnet,
		"!", "-o", BridgeName,
		"-j", "MASQUERADE",
	}
	err := exec.Command("iptables", args...).Run()
	return err == nil
}

// AllowForwarding — bridge üzərindən forwarding-ə icazə ver.
//
// Bəzi distro-larda FORWARD chain default DROP-dur, ona görə açıq əlavə edirik.
func AllowForwarding() error {
	rules := [][]string{
		// Bridge-dən gələn paketlərə icazə.
		{"-A", "FORWARD", "-i", BridgeName, "-j", "ACCEPT"},
		// Bridge-ə gedən paketlərə icazə.
		{"-A", "FORWARD", "-o", BridgeName, "-j", "ACCEPT"},
	}

	for _, rule := range rules {
		// Check-rule -A yerinə -C ilə.
		checkRule := append([]string{"-C"}, rule[1:]...)
		if exec.Command("iptables", checkRule...).Run() == nil {
			continue // artıq var
		}
		if err := runCmd("iptables", rule...); err != nil {
			return fmt.Errorf("FORWARD qaydası: %w", err)
		}
	}
	return nil
}

// runCmd — ümumi komanda runner (ip + iptables + nsenter üçün).
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (output: %s)",
			name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
