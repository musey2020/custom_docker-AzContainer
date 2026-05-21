// Package network — yüksək səviyyəli setup.
//
// Container start prosesində istifadə edilən birləşmiş funksiyalar.
package network

import (
	"fmt"
	"os"
)

// Network — bir container üçün şəbəkə resursları.
type Network struct {
	IP   string
	Veth *Veth
	ipam *IPAM
}

// SetupHost — host tərəfində (Run-da) bütün hazırlıq:
//   - bridge varmı? yoxsa yarat
//   - IP forwarding aktiv et
//   - NAT və FORWARD qaydaları
//   - IP allocate et
//   - veth pair yarat və host tərəfini bridge-ə qoş
//
// Container tərəfi hələ host namespace-dədir. Child spawn olduqdan sonra
// AttachToContainer çağırılmalıdır.
func SetupHost(containerID string) (*Network, error) {
	// 1. Bridge.
	if err := EnsureBridge(); err != nil {
		return nil, fmt.Errorf("bridge: %w", err)
	}

	// 2. IP forwarding.
	if err := EnableIPForward(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ip_forward: %v\n", err)
	}

	// 3. NAT.
	if err := EnsureNAT(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: NAT: %v (internet işləməyə bilər)\n", err)
	}

	// 4. FORWARD qaydaları.
	if err := AllowForwarding(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: forwarding: %v\n", err)
	}

	// 5. IP allocate.
	ipam, err := NewIPAM()
	if err != nil {
		return nil, fmt.Errorf("ipam: %w", err)
	}
	ip, err := ipam.Allocate()
	if err != nil {
		return nil, fmt.Errorf("IP allocate: %w", err)
	}

	// 6. veth pair.
	veth := NewVeth(containerID, ip)
	if err := veth.CreatePair(); err != nil {
		_ = ipam.Release(ip)
		return nil, fmt.Errorf("veth: %w", err)
	}

	// 7. Host tərəfini bridge-ə qoş.
	if err := veth.AttachToBridge(); err != nil {
		_ = veth.Cleanup()
		_ = ipam.Release(ip)
		return nil, fmt.Errorf("bridge-ə qoş: %w", err)
	}

	fmt.Printf("[host] Şəbəkə: IP=%s, veth=%s\n", ip, veth.HostName)

	return &Network{
		IP:   ip,
		Veth: veth,
		ipam: ipam,
	}, nil
}

// AttachToContainer — container PID-ə veth ucunu köçürür və konfiqurasiya edir.
//
// Bu, child spawn-dan SONRA, amma child-ın işə düşməsindən ƏVVƏL çağırılır.
// Yəni run() child Start-dan sonra, amma Wait-dən əvvəl.
func (n *Network) AttachToContainer(pid int) error {
	// 1. Container tərəfini ns-ə köçür.
	if err := n.Veth.MoveToNamespace(pid); err != nil {
		return fmt.Errorf("ns-ə köçür: %w", err)
	}

	// 2. Daxildə eth0 quraşdır.
	if err := n.Veth.ConfigureInsideNamespace(pid); err != nil {
		return fmt.Errorf("eth0 quraşdır: %w", err)
	}

	fmt.Printf("[host] Container şəbəkəsi hazır (eth0=%s)\n", n.IP)
	return nil
}

// Cleanup — şəbəkə resurslarını sil.
func (n *Network) Cleanup() error {
	if err := n.Veth.Cleanup(); err != nil {
		// Host tərəfi container ölümüylə özü silinə bilər.
	}
	return n.ipam.Release(n.IP)
}
