// Package state — container state idarəsi.
//
// Hər container haqqında məlumat /var/lib/azcontainer/state/<id>.json-da saxlanılır.
// Daemon restart olsa da, state qalır.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const StateDir = "/var/lib/azcontainer/state"

// Status — container vəziyyəti.
type Status string

const (
	StatusCreated Status = "created"
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusExited  Status = "exited"
)

// Container — bir container haqqında metadata.
type Container struct {
	ID        string    `json:"id"`      // tam ID (12 hex)
	Image     string    `json:"image"`   // istifadə olunan image adı
	Command   []string  `json:"command"` // çalışdırılan əmr
	PID       int       `json:"pid"`     // container PID-i (host-da görünən)
	Status    Status    `json:"status"`
	IP        string    `json:"ip,omitempty"` // container IP-si
	CreatedAt time.Time `json:"created_at"`
	ExitedAt  time.Time `json:"exited_at,omitempty"`
	ExitCode  int       `json:"exit_code"`
	LogPath   string    `json:"log_path"` // stdout/stderr log faylı
}

// Store — state idarəsi (disk-də saxlama).
type Store struct {
	mu  sync.RWMutex
	dir string
}

func NewStore() (*Store, error) {
	if err := os.MkdirAll(StateDir, 0755); err != nil {
		return nil, fmt.Errorf("state dir: %w", err)
	}
	return &Store{dir: StateDir}, nil
}

// Save — container state-ini diskə yazır.
func (s *Store) Save(c *Container) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, c.ID+".json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load — verilmiş ID üçün state oxuyur.
func (s *Store) Load(id string) (*Container, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("container tapılmadı: %s", id)
		}
		return nil, err
	}

	c := &Container{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	return c, nil
}

// List — bütün container-ləri qaytarır.
func (s *Store) List() ([]*Container, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var containers []*Container
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5] // ".json"-u kəs
		c, err := s.loadUnlocked(id)
		if err != nil {
			continue // pozulmuş state-i atla
		}
		containers = append(containers, c)
	}
	return containers, nil
}

func (s *Store) loadUnlocked(id string) (*Container, error) {
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Container{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Delete — container state-ini silir.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// FindByPrefix — qısa ID prefix-i ilə container tapır (Docker kimi).
//
// "abc123" prefix-i üçün "abc123def456" qaytarır.
func (s *Store) FindByPrefix(prefix string) (*Container, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}

	var matches []*Container
	for _, c := range all {
		if len(prefix) <= len(c.ID) && c.ID[:len(prefix)] == prefix {
			matches = append(matches, c)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("container tapılmadı: %s", prefix)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("birdən çox uyğunluq: %s", prefix)
	}
	return matches[0], nil
}

// IsAlive — verilmiş PID-də proses həqiqətən işləyirmi?
//
// signal 0 göndərmək prosesi dəyişdirmir, sadəcə mövcudluğu yoxlayır.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscallSignal0) == nil
}
