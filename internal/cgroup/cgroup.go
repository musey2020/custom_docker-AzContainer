// Package cgroup — cgroups v2 ilə resource limits.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	cgroupRoot  = "/sys/fs/cgroup"
	parentGroup = "azcontainer"
)

type Config struct {
	CPUQuota  string
	MemoryMax int64
	PidsMax   int
}

func DefaultConfig() Config {
	return Config{
		CPUQuota:  "50000 100000",
		MemoryMax: 256 * 1024 * 1024,
		PidsMax:   100,
	}
}

type Cgroup struct {
	Path string
}

// New — verilmiş container ID üçün cgroup yaradır və limit-ləri tətbiq edir.
func New(containerID string, cfg Config) (*Cgroup, error) {
	// ADDIM 1: Root cgroup-da controller-lər aktivdir?
	if err := ensureControllers(cgroupRoot); err != nil {
		return nil, fmt.Errorf("root controller-lər: %w", err)
	}

	// ADDIM 2: Parent cgroup (/sys/fs/cgroup/azcontainer) yarat.
	parentPath := filepath.Join(cgroupRoot, parentGroup)
	if err := os.MkdirAll(parentPath, 0755); err != nil {
		return nil, fmt.Errorf("parent cgroup yarat: %w", err)
	}

	// ADDIM 3: Parent cgroup-da da controller-ləri aktivləşdir.
	// Bu KRİTİKDİR — bunsuz child cgroup cpu.max və b. file-larına sahib olmur.
	if err := ensureControllers(parentPath); err != nil {
		return nil, fmt.Errorf("parent controller-lər: %w", err)
	}

	// ADDIM 4: Child cgroup yarat.
	cgroupPath := filepath.Join(parentPath, containerID)
	if err := os.MkdirAll(cgroupPath, 0755); err != nil {
		return nil, fmt.Errorf("child cgroup yarat: %w", err)
	}

	c := &Cgroup{Path: cgroupPath}

	// ADDIM 5: Limit-ləri tətbiq et.
	if err := c.applyLimits(cfg); err != nil {
		_ = c.Delete()
		return nil, fmt.Errorf("limit-lər tətbiq: %w", err)
	}

	return c, nil
}

// ensureControllers — verilmiş cgroup-un subtree_control-ında lazımi
// controller-ləri aktivləşdirir. Artıq aktivdirsə, heç nə etmir.
func ensureControllers(cgroupPath string) error {
	subtreePath := filepath.Join(cgroupPath, "cgroup.subtree_control")

	// Mövcud controller-ləri oxu.
	current, err := os.ReadFile(subtreePath)
	if err != nil {
		return fmt.Errorf("subtree_control oxu: %w", err)
	}
	currentStr := string(current)

	// Hansı controller-lər lazımdır?
	required := []string{"cpu", "memory", "pids"}
	var toEnable []string
	for _, ctrl := range required {
		if !strings.Contains(currentStr, ctrl) {
			toEnable = append(toEnable, "+"+ctrl)
		}
	}

	// Hamısı aktivdirsə, heç nə etmə.
	if len(toEnable) == 0 {
		return nil
	}

	// Aktivləşdir.
	value := strings.Join(toEnable, " ")
	if err := os.WriteFile(subtreePath, []byte(value), 0644); err != nil {
		return fmt.Errorf("subtree_control yaz (%s): %w", value, err)
	}

	return nil
}

func (c *Cgroup) applyLimits(cfg Config) error {
	if cfg.CPUQuota != "" {
		if err := c.writeFile("cpu.max", cfg.CPUQuota); err != nil {
			return fmt.Errorf("cpu.max yaz: %w", err)
		}
	}

	if cfg.MemoryMax > 0 {
		value := strconv.FormatInt(cfg.MemoryMax, 10)
		if err := c.writeFile("memory.max", value); err != nil {
			return fmt.Errorf("memory.max yaz: %w", err)
		}
	}

	if cfg.PidsMax > 0 {
		value := strconv.Itoa(cfg.PidsMax)
		if err := c.writeFile("pids.max", value); err != nil {
			return fmt.Errorf("pids.max yaz: %w", err)
		}
	}

	return nil
}

func (c *Cgroup) AddProcess(pid int) error {
	pidStr := strconv.Itoa(pid)
	if err := c.writeFile("cgroup.procs", pidStr); err != nil {
		return fmt.Errorf("PID %d cgroup-a əlavə: %w", pid, err)
	}
	return nil
}

func (c *Cgroup) Delete() error {
	if err := os.Remove(c.Path); err != nil {
		return fmt.Errorf("cgroup sil: %w", err)
	}
	return nil
}

func (c *Cgroup) writeFile(name, value string) error {
	path := filepath.Join(c.Path, name)
	return os.WriteFile(path, []byte(value), 0644)
}
