// Package daemon — RPC API.
//
// Bu fayl CLI və daemon arasında shared types və RPC method-ları təyin edir.
// net/rpc + jsonrpc istifadə edirik (gRPC əvəzinə — internet yoxdur).
package daemon

import (
	"azcontainer/internal/runtime"
	"azcontainer/internal/state"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

// Socket — daemon-un dinlədiyi Unix socket path-i.
const Socket = "/var/run/azcontainer.sock"

// — RPC argument tipləri —
//
// net/rpc tələb edir ki, hər method exported olsun və 2 arg + 1 reply alsın.

type RunArgs struct {
	Image   string
	Command []string
}

type RunReply struct {
	ID  string
	PID int
	IP  string
}

type ListReply struct {
	Containers []*state.Container
}

type IDArgs struct {
	ID string // tam və ya prefix
}

type EmptyReply struct{}

type LogsReply struct {
	Content string
}

// API — RPC service.
//
// Daemon bu struct-ın methodlarını expose edir.
// Hər method imza: func(args T, reply *R) error
type API struct {
	mu      sync.Mutex
	handles map[string]*runtime.Handle // ID → Handle
	store   *state.Store
}

func NewAPI() (*API, error) {
	store, err := state.NewStore()
	if err != nil {
		return nil, err
	}
	return &API{
		handles: make(map[string]*runtime.Handle),
		store:   store,
	}, nil
}

// Run — yeni container işə salır.
func (a *API) Run(args *RunArgs, reply *RunReply) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if args.Image == "" {
		return fmt.Errorf("image tələb olunur")
	}

	s, err := runtime.PrepareSpec(args.Image, args.Command)
	if err != nil {
		return err
	}

	handle, err := runtime.StartDetached(s, args.Image)
	if err != nil {
		return err
	}

	a.handles[handle.State.ID] = handle

	// Ayrı goroutine-də container-i izlə.
	go func(h *runtime.Handle) {
		_ = h.Wait()
		a.mu.Lock()
		delete(a.handles, h.State.ID)
		a.mu.Unlock()
	}(handle)

	reply.ID = handle.State.ID
	reply.PID = handle.State.PID
	reply.IP = handle.State.IP
	return nil
}

// List — bütün container-ləri qaytarır (running və exited).
func (a *API) List(_ *struct{}, reply *ListReply) error {
	containers, err := a.store.List()
	if err != nil {
		return err
	}

	// Status-ları yenilə — proses həqiqətən yaşayırmı?
	for _, c := range containers {
		if c.Status == state.StatusRunning && !state.IsAlive(c.PID) {
			c.Status = state.StatusExited
			c.ExitedAt = time.Now()
			_ = a.store.Save(c)
		}
	}

	reply.Containers = containers
	return nil
}

// Stop — container-ə SIGTERM göndərir. 5 saniyə sonra SIGKILL.
func (a *API) Stop(args *IDArgs, _ *EmptyReply) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	c, err := a.store.FindByPrefix(args.ID)
	if err != nil {
		return err
	}

	handle, ok := a.handles[c.ID]
	if !ok {
		// Daemon restart olubsa, handle yoxdur. Birbaşa PID-ə signal.
		if c.PID > 0 && state.IsAlive(c.PID) {
			proc, err := os.FindProcess(c.PID)
			if err == nil {
				_ = proc.Signal(syscall.SIGTERM)
				time.Sleep(5 * time.Second)
				if state.IsAlive(c.PID) {
					_ = proc.Signal(syscall.SIGKILL)
				}
			}
		}
		return nil
	}

	if err := handle.Kill(syscall.SIGTERM); err != nil {
		return err
	}

	// Grace period.
	go func() {
		time.Sleep(5 * time.Second)
		a.mu.Lock()
		if h, ok := a.handles[c.ID]; ok {
			_ = h.Kill(syscall.SIGKILL)
		}
		a.mu.Unlock()
	}()

	return nil
}

// Remove — exited container-i silir.
func (a *API) Remove(args *IDArgs, _ *EmptyReply) error {
	c, err := a.store.FindByPrefix(args.ID)
	if err != nil {
		return err
	}

	if c.Status == state.StatusRunning {
		return fmt.Errorf("container işləyir, əvvəlcə stop et")
	}

	// Log faylı silinsin.
	if c.LogPath != "" {
		_ = os.Remove(c.LogPath)
	}

	return a.store.Delete(c.ID)
}

// Logs — container log-larını qaytarır.
func (a *API) Logs(args *IDArgs, reply *LogsReply) error {
	c, err := a.store.FindByPrefix(args.ID)
	if err != nil {
		return err
	}

	f, err := os.Open(c.LogPath)
	if err != nil {
		return fmt.Errorf("log oxu: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	reply.Content = string(data)
	return nil
}
