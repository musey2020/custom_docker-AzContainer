// Package network — veth pair yarat və container-ə ötür.
//
// veth ("virtual ethernet") iki ucu olan virtual kabel-dir:
//   - bir uc host-da qalır və bridge-ə qoşulur
//   - digər uc container-in network namespace-inə köçürülür və eth0 olur
//
// Bu, Docker-in standart yanaşmasıdır.
package network

import (
	"fmt"
)

// Veth — bir container üçün veth pair-in identifikatorları.
type Veth struct {
	HostName      string // host tərəfində ad (məsələn "veth-abc")
	ContainerName string // container tərəfində ad (məsələn "ceth-abc")
	ContainerIP   string // container-in IP-si
}

// NewVeth — verilmiş container ID üçün veth pair yaradır.
//
// containerID-nin ilk 6 simvolunu istifadə edirik, çünki Linux interface
// adı 15 simvoldan çox olmamalıdır.
func NewVeth(containerID, ip string) *Veth {
	short := containerID
	if len(short) > 6 {
		short = short[:6]
	}
	return &Veth{
		HostName:      "veth-" + short, // 11 simvol
		ContainerName: "ceth-" + short, // 11 simvol
		ContainerIP:   ip,
	}
}

// CreatePair — host tərəfində veth pair yaradır.
//
// İki uc da host namespace-də yaranır. Sonra ContainerName ucunu
// container PID-nin network ns-inə köçürürük (MoveToNamespace).
func (v *Veth) CreatePair() error {
	// ip link add <host> type veth peer name <container>
	if err := runIP("link", "add", v.HostName, "type", "veth",
		"peer", "name", v.ContainerName); err != nil {
		return fmt.Errorf("veth pair yarat: %w", err)
	}
	return nil
}

// AttachToBridge — host tərəfini bridge-ə qoşur və aktivləşdirir.
func (v *Veth) AttachToBridge() error {
	// ip link set <host> master <bridge>
	if err := runIP("link", "set", v.HostName, "master", BridgeName); err != nil {
		return fmt.Errorf("bridge-ə qoş: %w", err)
	}
	// ip link set <host> up
	if err := runIP("link", "set", v.HostName, "up"); err != nil {
		return fmt.Errorf("host UP: %w", err)
	}
	return nil
}

// MoveToNamespace — container ucunu PID-in network ns-inə köçürür.
//
// Trick: hər prosesin /proc/<pid>/ns/net əlamətli bir ns-i var.
// `ip link set X netns <pid>` bu nömrəyə əsasən köçürür.
func (v *Veth) MoveToNamespace(pid int) error {
	// ip link set <container> netns <pid>
	if err := runIP("link", "set", v.ContainerName, "netns",
		fmt.Sprintf("%d", pid)); err != nil {
		return fmt.Errorf("netns-ə köçür: %w", err)
	}
	return nil
}

// ConfigureInsideNamespace — container-in network ns-i daxilində interface-i quraşdırır.
//
// `ip netns exec` istifadə edə bilmirik, çünki ns adlanmayıb (PID ilə deyil ad ilə).
// Onun yerinə `nsenter -t <pid> -n` istifadə edirik.
func (v *Veth) ConfigureInsideNamespace(pid int) error {
	pidStr := fmt.Sprintf("%d", pid)

	// 1. Interface adını "eth0"-a dəyiş (Docker bu adı işlədir).
	if err := nsenterIP(pidStr, "link", "set", v.ContainerName, "name", "eth0"); err != nil {
		return fmt.Errorf("ad dəyiş: %w", err)
	}

	// 2. IP təyin et.
	cidr := fmt.Sprintf("%s/%d", v.ContainerIP, NetmaskBits)
	if err := nsenterIP(pidStr, "addr", "add", cidr, "dev", "eth0"); err != nil {
		return fmt.Errorf("IP təyin: %w", err)
	}

	// 3. eth0-ı aktivləşdir.
	if err := nsenterIP(pidStr, "link", "set", "eth0", "up"); err != nil {
		return fmt.Errorf("eth0 UP: %w", err)
	}

	// 4. loopback-i aktivləşdir.
	if err := nsenterIP(pidStr, "link", "set", "lo", "up"); err != nil {
		return fmt.Errorf("lo UP: %w", err)
	}

	// 5. Default route — bridge IP-si.
	if err := nsenterIP(pidStr, "route", "add", "default", "via", BridgeIP); err != nil {
		return fmt.Errorf("default route: %w", err)
	}

	return nil
}

// Cleanup — host tərəfindəki veth-i silir (digər ucu container ölürkən özü gedir).
func (v *Veth) Cleanup() error {
	// ip link delete <host>
	if err := runIP("link", "delete", v.HostName); err != nil {
		// Host tərəfi yoxdursa, ignore et.
		return nil
	}
	return nil
}

// nsenterIP — `nsenter -t <pid> -n -- ip ...` çağırır.
func nsenterIP(pid string, args ...string) error {
	fullArgs := append([]string{"-t", pid, "-n", "--", "ip"}, args...)
	return runCmd("nsenter", fullArgs...)
}
