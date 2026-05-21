// Package runtime — detached container yaratma (daemon üçün).
package runtime

import (
	"azcontainer/internal/cgroup"
	"azcontainer/internal/filesystem"
	"azcontainer/internal/image"
	"azcontainer/internal/network"
	"azcontainer/internal/spec"
	"azcontainer/internal/state"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Handle — başladılmış container-in cleanup üçün lazımi resursları.
type Handle struct {
	State   *state.Container
	Layout  *filesystem.LayoutPaths
	Cgroup  *cgroup.Cgroup
	Network *network.Network
	cmd     *exec.Cmd
	logFile *os.File
}

// StartDetached — container-i background-da işə salır və dərhal qayıdır.
//
// Daemon bu funksiyanı çağırır. Container yaranır, PID alınır, state yazılır.
// Bunun gözləmə hissəsini (cmd.Wait) daemon ayrı goroutine-də edir.
func StartDetached(s *spec.Spec, imageName string) (*Handle, error) {
	containerID := s.Hostname
	if containerID == "" {
		id, err := generateContainerID()
		if err != nil {
			return nil, err
		}
		containerID = id
		s.Hostname = id
	}

	// State store və log directory.
	store, err := state.NewStore()
	if err != nil {
		return nil, fmt.Errorf("state store: %w", err)
	}

	logDir := "/var/lib/azcontainer/logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("log dir: %w", err)
	}
	logPath := fmt.Sprintf("%s/%s.log", logDir, containerID)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("log fayl: %w", err)
	}

	// İlkin state.
	cState := &state.Container{
		ID:        containerID,
		Image:     imageName,
		Command:   s.Process.Args,
		Status:    state.StatusCreated,
		CreatedAt: time.Now(),
		LogPath:   logPath,
	}
	if err := store.Save(cState); err != nil {
		logFile.Close()
		return nil, err
	}

	// OverlayFS.
	layout, err := filesystem.NewLayoutForImage(containerID, s.Root.Path)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("layout: %w", err)
	}
	if err := filesystem.MountOverlay(layout); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("overlay: %w", err)
	}

	// Cgroup.
	cg, err := cgroup.New(containerID, cgroupConfigFromSpec(s))
	if err != nil {
		_ = filesystem.UnmountOverlay(layout)
		_ = filesystem.Cleanup(layout)
		logFile.Close()
		return nil, fmt.Errorf("cgroup: %w", err)
	}

	// Network.
	var net *network.Network
	if s.HasNamespace(spec.NamespaceNetwork) {
		net, err = network.SetupHost(containerID)
		if err != nil {
			_ = cg.Delete()
			_ = filesystem.UnmountOverlay(layout)
			_ = filesystem.Cleanup(layout)
			logFile.Close()
			return nil, fmt.Errorf("network: %w", err)
		}
		cState.IP = net.IP
	}

	// Env hazırla.
	env := append([]string{}, s.Process.Env...)
	env = append(env, "HOSTNAME="+containerID)
	env = append(env, "PS1=\\u@"+containerID+":\\w # ")

	readyR, readyW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	initArgs := []string{"init", containerID, layout.Merged, s.Process.Cwd,
		fmt.Sprintf("%d", len(env))}
	initArgs = append(initArgs, env...)
	initArgs = append(initArgs, s.Process.Args...)

	cmd := exec.Command("/proc/self/exe", initArgs...)
	// stdout/stderr-i log fayla yönləndir (detached üçün).
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = cloneFlagsFromSpec(s)
	cmd.ExtraFiles = []*os.File{readyR}

	if err := cmd.Start(); err != nil {
		readyW.Close()
		return nil, fmt.Errorf("start: %w", err)
	}
	readyR.Close()

	if err := cg.AddProcess(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("cgroup add: %w", err)
	}

	if net != nil {
		if err := net.AttachToContainer(cmd.Process.Pid); err != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("network attach: %w", err)
		}
	}

	// Child-a icazə ver.
	readyW.Write([]byte{1})
	readyW.Close()

	// State-i yenilə.
	cState.PID = cmd.Process.Pid
	cState.Status = state.StatusRunning
	if err := store.Save(cState); err != nil {
		return nil, err
	}

	return &Handle{
		State:   cState,
		Layout:  layout,
		Cgroup:  cg,
		Network: net,
		cmd:     cmd,
		logFile: logFile,
	}, nil
}

// Wait — handle-də olan container ölənə qədər gözləyir və cleanup edir.
//
// Daemon bunu ayrı goroutine-də çağırır.
func (h *Handle) Wait() error {
	err := h.cmd.Wait()

	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}

	// State-i yenilə.
	store, _ := state.NewStore()
	h.State.Status = state.StatusExited
	h.State.ExitedAt = time.Now()
	h.State.ExitCode = exitCode
	_ = store.Save(h.State)

	// Cleanup.
	h.cleanup()
	return err
}

// Kill — container-ə signal göndərir.
func (h *Handle) Kill(sig syscall.Signal) error {
	if h.cmd.Process == nil {
		return fmt.Errorf("proses yoxdur")
	}
	return h.cmd.Process.Signal(sig)
}

// cleanup — bütün resursları sil.
func (h *Handle) cleanup() {
	if h.Network != nil {
		_ = h.Network.Cleanup()
	}
	if h.Cgroup != nil {
		_ = h.Cgroup.Delete()
	}
	if h.Layout != nil {
		_ = filesystem.UnmountOverlay(h.Layout)
		_ = filesystem.Cleanup(h.Layout)
	}
	if h.logFile != nil {
		_ = h.logFile.Close()
	}
}

// PrepareSpec — image-dən və user komandadan spec hazırlayır.
//
// CLI-də və daemon-da istifadə olunan ümumi yol.
func PrepareSpec(imageName string, userCmd []string) (*spec.Spec, error) {
	store, err := image.NewStore()
	if err != nil {
		return nil, err
	}

	_, imgConfig, err := store.LoadImage(imageName)
	if err != nil {
		return nil, fmt.Errorf("image %q: %w", imageName, err)
	}

	rootfsPath := store.ImageRootFS(imageName)
	if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
		if err := store.AssembleRootFS(imageName); err != nil {
			return nil, fmt.Errorf("rootfs: %w", err)
		}
	}

	hostname, err := generateContainerID()
	if err != nil {
		return nil, err
	}

	s := spec.DefaultSpec(imgConfig, rootfsPath, hostname)
	if len(userCmd) > 0 {
		s.Process.Args = userCmd
	}
	return s, nil
}
