// Package runtime — container lifecycle.
//
// Mərhələ 7: Networking əlavə edildi.
package runtime

import (
	"azcontainer/internal/cgroup"
	"azcontainer/internal/filesystem"
	"azcontainer/internal/image"
	"azcontainer/internal/network"
	"azcontainer/internal/security"
	"azcontainer/internal/spec"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("image adı və ya --spec tələb olunur")
	}

	var s *spec.Spec
	var err error

	if args[0] == "--spec" {
		if len(args) < 2 {
			return fmt.Errorf("--spec üçün path lazımdır")
		}
		s, err = spec.Load(args[1])
		if err != nil {
			return fmt.Errorf("spec yüklə: %w", err)
		}
	} else {
		s, err = specFromImage(args[0], args[1:])
		if err != nil {
			return err
		}
	}

	return runWithSpec(s)
}

func specFromImage(imageName string, userCmd []string) (*spec.Spec, error) {
	store, err := image.NewStore()
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	_, imgConfig, err := store.LoadImage(imageName)
	if err != nil {
		return nil, fmt.Errorf("image %q yüklə: %w", imageName, err)
	}

	rootfsPath := store.ImageRootFS(imageName)
	if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
		fmt.Println("Rootfs hazırlanır...")
		if err := store.AssembleRootFS(imageName); err != nil {
			return nil, fmt.Errorf("rootfs assemble: %w", err)
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

func runWithSpec(s *spec.Spec) error {
	containerID := s.Hostname
	if containerID == "" {
		id, err := generateContainerID()
		if err != nil {
			return err
		}
		containerID = id
		s.Hostname = id
	}
	fmt.Printf("[host] Container ID: %s\n", containerID)

	// OverlayFS.
	layout, err := filesystem.NewLayoutForImage(containerID, s.Root.Path)
	if err != nil {
		return fmt.Errorf("layout: %w", err)
	}
	if err := filesystem.MountOverlay(layout); err != nil {
		return fmt.Errorf("overlay mount: %w", err)
	}
	fmt.Println("[host] OverlayFS mount edildi")

	// Cgroup.
	cg, err := cgroup.New(containerID, cgroupConfigFromSpec(s))
	if err != nil {
		_ = filesystem.UnmountOverlay(layout)
		_ = filesystem.Cleanup(layout)
		return fmt.Errorf("cgroup: %w", err)
	}
	fmt.Printf("[host] Cgroup: %s\n", cg.Path)

	// Network — spec network namespace istəyirsə setup et.
	var net *network.Network
	if s.HasNamespace(spec.NamespaceNetwork) {
		net, err = network.SetupHost(containerID)
		if err != nil {
			_ = cg.Delete()
			_ = filesystem.UnmountOverlay(layout)
			_ = filesystem.Cleanup(layout)
			return fmt.Errorf("network setup: %w", err)
		}
	}

	// Cleanup defer — sıralama önəmlidir.
	defer func() {
		fmt.Println("[host] Cleanup başlayır")
		if net != nil {
			if err := net.Cleanup(); err != nil {
				fmt.Fprintf(os.Stderr, "[host] net cleanup: %v\n", err)
			}
		}
		if err := cg.Delete(); err != nil {
			fmt.Fprintf(os.Stderr, "[host] cgroup sil: %v\n", err)
		}
		if err := filesystem.UnmountOverlay(layout); err != nil {
			fmt.Fprintf(os.Stderr, "[host] unmount: %v\n", err)
		}
		if err := filesystem.Cleanup(layout); err != nil {
			fmt.Fprintf(os.Stderr, "[host] cleanup: %v\n", err)
		}
		fmt.Println("[host] Cleanup bitdi")
	}()

	// Env.
	env := append([]string{}, s.Process.Env...)
	env = append(env, "HOSTNAME="+containerID)
	env = append(env, "PS1=\\u@"+containerID+":\\w # ")

	// Sinxronizasiya pipe — host network setup edənə qədər child gözləyir.
	// Child eth0 hazır olmadan exec etsə, network olmayacaq.
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}

	initArgs := []string{"init", containerID, layout.Merged, s.Process.Cwd,
		fmt.Sprintf("%d", len(env))}
	initArgs = append(initArgs, env...)
	initArgs = append(initArgs, s.Process.Args...)

	cmd := exec.Command("/proc/self/exe", initArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = cloneFlagsFromSpec(s)

	// Pipe-i child-a ötür (FD 3 olacaq).
	cmd.ExtraFiles = []*os.File{readyR}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("container start: %w", err)
	}

	// Host-da pipe-in oxuma ucu artıq lazım deyil.
	readyR.Close()

	if err := cg.AddProcess(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("cgroup add: %w", err)
	}
	fmt.Printf("[host] PID %d cgroup-a əlavə edildi\n", cmd.Process.Pid)

	// İndi network-i container-ə qoş.
	if net != nil {
		if err := net.AttachToContainer(cmd.Process.Pid); err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("network attach: %w", err)
		}
	}

	// Child-a signal göndər: hər şey hazırdır.
	readyW.Write([]byte{1})
	readyW.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("container: %w", err)
	}
	return nil
}

func cgroupConfigFromSpec(s *spec.Spec) cgroup.Config {
	cfg := cgroup.DefaultConfig()
	if s.Linux == nil || s.Linux.Resources == nil {
		return cfg
	}
	r := s.Linux.Resources
	if r.CPU != nil && r.CPU.Quota != nil && r.CPU.Period != nil {
		cfg.CPUQuota = fmt.Sprintf("%d %d", *r.CPU.Quota, *r.CPU.Period)
	}
	if r.Memory != nil && r.Memory.Limit != nil {
		cfg.MemoryMax = *r.Memory.Limit
	}
	if r.Pids != nil && r.Pids.Limit > 0 {
		cfg.PidsMax = int(r.Pids.Limit)
	}
	return cfg
}

func cloneFlagsFromSpec(s *spec.Spec) *syscall.SysProcAttr {
	var flags uintptr
	if s.HasNamespace(spec.NamespacePID) {
		flags |= syscall.CLONE_NEWPID
	}
	if s.HasNamespace(spec.NamespaceNetwork) {
		flags |= syscall.CLONE_NEWNET
	}
	if s.HasNamespace(spec.NamespaceMount) {
		flags |= syscall.CLONE_NEWNS
	}
	if s.HasNamespace(spec.NamespaceIPC) {
		flags |= syscall.CLONE_NEWIPC
	}
	if s.HasNamespace(spec.NamespaceUTS) {
		flags |= syscall.CLONE_NEWUTS
	}
	if s.HasNamespace(spec.NamespaceCgroup) {
		flags |= syscall.CLONE_NEWCGROUP
	}
	return &syscall.SysProcAttr{Cloneflags: flags}
}

// Init — child funksiyası.
//
// Network qoşulana qədər FD 3-dən gözləyirik (sinxronizasiya).
func Init(args []string) error {
	if len(args) < 5 {
		return fmt.Errorf("init: çatışmayan arqumentlər")
	}

	containerID := args[0]
	mergedPath := args[1]
	workdir := args[2]

	envCount := 0
	fmt.Sscanf(args[3], "%d", &envCount)
	if 4+envCount > len(args) {
		return fmt.Errorf("init: env count uyğunsuzluğu")
	}
	env := args[4 : 4+envCount]
	cmdArgs := args[4+envCount:]
	if len(cmdArgs) == 0 {
		return fmt.Errorf("init: komanda boşdur")
	}

	// Host-un signal-ını gözlə (FD 3).
	// Host network setup edənə qədər heç bir syscall edə bilmərik.
	readyR := os.NewFile(3, "ready")
	if readyR != nil {
		buf := make([]byte, 1)
		readyR.Read(buf)
		readyR.Close()
	}

	if err := syscall.Sethostname([]byte(containerID)); err != nil {
		return fmt.Errorf("sethostname: %w", err)
	}

	if err := filesystem.PivotRoot(mergedPath); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	if err := filesystem.MountVirtualFilesystems(); err != nil {
		return fmt.Errorf("virtual fs: %w", err)
	}

	if workdir != "" && workdir != "/" {
		if err := os.Chdir(workdir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: chdir %s: %v\n", workdir, err)
		}
	}

	if err := security.Apply(); err != nil {
		return fmt.Errorf("security: %w", err)
	}

	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			os.Setenv("PATH", strings.TrimPrefix(e, "PATH="))
			break
		}
	}

	binary, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return fmt.Errorf("%s tapılmadı: %w", cmdArgs[0], err)
	}

	return syscall.Exec(binary, cmdArgs, env)
}

func generateContainerID() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
