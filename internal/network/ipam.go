// Package network — IP address management (IPAM).
//
// Sadə yanaşma: bir subnet (10.88.0.0/16), bir counter.
// Bridge .1, container-lər .2-dən başlayır.
//
// State persist olunmur — daemon olmadığı üçün hər run-da yeni IP
// allocate etmək olar. Production-da state file lazımdır.
package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	// Subnet — bütün container-lər bu subnet-də olacaq.
	Subnet      = "10.88.0.0/16"
	BridgeIP    = "10.88.0.1"
	BridgeCIDR  = "10.88.0.1/16"
	NetmaskBits = 16

	// State faylı — istifadə olunan IP-ləri saxlayır.
	stateFile = "/var/lib/azcontainer/network/allocated.txt"
)

// IPAM — IP allocation manager.
type IPAM struct {
	mu        sync.Mutex
	allocated map[string]bool // IP → istifadədə?
}

// NewIPAM — state-i diskdən oxuyur.
func NewIPAM() (*IPAM, error) {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return nil, fmt.Errorf("state dir: %w", err)
	}

	ipam := &IPAM{
		allocated: make(map[string]bool),
	}

	// Bridge IP-si həmişə "istifadədə".
	ipam.allocated[BridgeIP] = true

	// Köhnə state-i oxu (varsa).
	data, err := os.ReadFile(stateFile)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				ipam.allocated[line] = true
			}
		}
	}

	return ipam, nil
}

// Allocate — istifadə olunmamış IP qaytarır.
//
// Sadə alqoritm: 10.88.0.2-dən başlayıb 10.88.255.254-ə qədər axtarır.
func (i *IPAM) Allocate() (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// 10.88.0.2-dən başla.
	for octet3 := 0; octet3 < 256; octet3++ {
		for octet4 := 2; octet4 < 255; octet4++ {
			ip := fmt.Sprintf("10.88.%d.%d", octet3, octet4)
			if !i.allocated[ip] {
				i.allocated[ip] = true
				if err := i.persist(); err != nil {
					return "", err
				}
				return ip, nil
			}
		}
	}
	return "", fmt.Errorf("subnet-də boş IP yoxdur")
}

// Release — IP-ni geri qaytarır.
func (i *IPAM) Release(ip string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.allocated, ip)
	return i.persist()
}

// persist — allocated map-i diskə yazır.
func (i *IPAM) persist() error {
	var lines []string
	for ip := range i.allocated {
		if ip == BridgeIP {
			continue // bridge-i state-də saxlamırıq
		}
		lines = append(lines, ip)
	}
	return os.WriteFile(stateFile, []byte(strings.Join(lines, "\n")), 0644)
}

// ipToInt — debug üçün.
func ipToInt(ip string) (uint32, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0, fmt.Errorf("yanlış IP: %s", ip)
	}
	var result uint32
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return 0, fmt.Errorf("yanlış oktet: %s", p)
		}
		result = (result << 8) | uint32(n)
	}
	return result, nil
}
