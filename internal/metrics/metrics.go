// Package metrics — container metric-ləri.
//
// Cgroup v2-dən resurs istifadəsi oxuyur:
//
//	/sys/fs/cgroup/azcontainer/<id>/cpu.stat       → CPU
//	/sys/fs/cgroup/azcontainer/<id>/memory.current → RAM
//	/sys/fs/cgroup/azcontainer/<id>/pids.current   → PID sayı
package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const cgroupBase = "/sys/fs/cgroup/azcontainer"

// Snapshot — bir an üçün container resursları.
type Snapshot struct {
	ContainerID    string    `json:"container_id"`
	Timestamp      time.Time `json:"timestamp"`
	CPUUsageNanos  uint64    `json:"cpu_usage_nanos"`  // ümumi CPU vaxtı (ns)
	MemoryBytes    uint64    `json:"memory_bytes"`     // hazırkı RAM
	MemoryMaxBytes uint64    `json:"memory_max_bytes"` // pik RAM
	PIDsCurrent    int       `json:"pids_current"`     // hazırkı PID sayı
	PIDsMax        int       `json:"pids_max"`         // limit
}

// Collect — verilmiş container üçün metric snapshot oxuyur.
func Collect(containerID string) (*Snapshot, error) {
	dir := filepath.Join(cgroupBase, containerID)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("cgroup tapılmadı: %s", containerID)
	}

	s := &Snapshot{
		ContainerID: containerID,
		Timestamp:   time.Now(),
	}

	// CPU stats — cpu.stat file format:
	//   usage_usec 12345
	//   user_usec 8000
	//   system_usec 4345
	if data, err := os.ReadFile(filepath.Join(dir, "cpu.stat")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[0] == "usage_usec" {
				usec, _ := strconv.ParseUint(parts[1], 10, 64)
				s.CPUUsageNanos = usec * 1000 // µs → ns
			}
		}
	}

	// Memory.
	if data, err := os.ReadFile(filepath.Join(dir, "memory.current")); err == nil {
		s.MemoryBytes, _ = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "memory.peak")); err == nil {
		s.MemoryMaxBytes, _ = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	}

	// PIDs.
	if data, err := os.ReadFile(filepath.Join(dir, "pids.current")); err == nil {
		s.PIDsCurrent, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if data, err := os.ReadFile(filepath.Join(dir, "pids.max")); err == nil {
		v := strings.TrimSpace(string(data))
		if v != "max" {
			s.PIDsMax, _ = strconv.Atoi(v)
		}
	}

	return s, nil
}

// FormatHuman — insan oxuya bilən format (CLI üçün).
func (s *Snapshot) FormatHuman() string {
	mem := formatBytes(s.MemoryBytes)
	memPeak := formatBytes(s.MemoryMaxBytes)
	cpu := time.Duration(s.CPUUsageNanos).Round(time.Millisecond)

	pids := fmt.Sprintf("%d", s.PIDsCurrent)
	if s.PIDsMax > 0 {
		pids = fmt.Sprintf("%d/%d", s.PIDsCurrent, s.PIDsMax)
	}

	return fmt.Sprintf("CPU=%v  MEM=%s (peak %s)  PIDs=%s",
		cpu, mem, memPeak, pids)
}

// FormatPrometheus — Prometheus exposition format.
//
// /metrics endpoint-i bu format-ı qaytarır.
func (s *Snapshot) FormatPrometheus() string {
	var sb strings.Builder
	id := s.ContainerID
	fmt.Fprintf(&sb, "azcontainer_cpu_usage_nanoseconds{container=%q} %d\n", id, s.CPUUsageNanos)
	fmt.Fprintf(&sb, "azcontainer_memory_bytes{container=%q} %d\n", id, s.MemoryBytes)
	fmt.Fprintf(&sb, "azcontainer_memory_peak_bytes{container=%q} %d\n", id, s.MemoryMaxBytes)
	fmt.Fprintf(&sb, "azcontainer_pids_current{container=%q} %d\n", id, s.PIDsCurrent)
	fmt.Fprintf(&sb, "azcontainer_pids_max{container=%q} %d\n", id, s.PIDsMax)
	return sb.String()
}

func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
