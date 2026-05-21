// Package spec — image config-dən default spec generasiyası.
package spec

import (
	"azcontainer/internal/image"
)

// DefaultSpec — image config-ə əsaslanaraq sensible default spec qaytarır.
//
// Bu, Docker-in `docker run` default-larına yaxındır:
//   - bütün namespace-lər
//   - default cgroup limits (CPU 50%, RAM 256MB, PIDs 100)
//   - capabilities = Docker default whitelist
//   - noNewPrivileges = true
//   - default mount-lar (/proc, /sys, /dev)
func DefaultSpec(imgConfig *image.Config, rootfsPath, hostname string) *Spec {
	// Komanda: image-dən gələn entrypoint + cmd.
	args := append([]string{}, imgConfig.Config.Entrypoint...)
	args = append(args, imgConfig.Config.Cmd...)

	// Env.
	env := append([]string{}, imgConfig.Config.Env...)
	if len(env) == 0 {
		env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}

	// WorkingDir.
	cwd := imgConfig.Config.WorkingDir
	if cwd == "" {
		cwd = "/"
	}

	// Default cgroup limits (uint64/int64 pointer-lər lazımdır).
	memLimit := int64(256 * 1024 * 1024) // 256MB
	cpuQuota := int64(50000)             // 50%
	cpuPeriod := uint64(100000)

	return &Spec{
		OCIVersion: "1.0.2",
		Hostname:   hostname,
		Process: &Process{
			Terminal:        true,
			User:            User{UID: 0, GID: 0},
			Args:            args,
			Env:             env,
			Cwd:             cwd,
			NoNewPrivileges: true,
			Capabilities:    defaultCapabilities(),
		},
		Root: &Root{
			Path:     rootfsPath,
			Readonly: false,
		},
		Mounts: defaultMounts(),
		Linux: &Linux{
			Namespaces: []Namespace{
				{Type: NamespacePID},
				{Type: NamespaceNetwork},
				{Type: NamespaceMount},
				{Type: NamespaceIPC},
				{Type: NamespaceUTS},
				{Type: NamespaceCgroup},
			},
			Resources: &Resources{
				Memory: &Memory{Limit: &memLimit},
				CPU:    &CPU{Quota: &cpuQuota, Period: &cpuPeriod},
				Pids:   &Pids{Limit: 100},
			},
		},
	}
}

// defaultCapabilities — Docker default whitelist.
//
// security/capabilities.go-da olan siyahı ilə eyni olmalıdır.
// Burada string format-da, çünki OCI spec belə tələb edir.
func defaultCapabilities() *Capabilities {
	caps := []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FOWNER",
		"CAP_FSETID",
		"CAP_KILL",
		"CAP_SETGID",
		"CAP_SETUID",
		"CAP_SETPCAP",
		"CAP_NET_BIND_SERVICE",
		"CAP_SYS_CHROOT",
		"CAP_MKNOD",
		"CAP_AUDIT_WRITE",
		"CAP_SETFCAP",
	}
	return &Capabilities{
		Bounding:    caps,
		Effective:   caps,
		Permitted:   caps,
		Inheritable: []string{}, // default-da heç biri inheritable deyil
		Ambient:     []string{},
	}
}

// defaultMounts — standart container mount-ları.
//
// Bu mount-lar pivot_root-dan sonra container daxilində qurulur.
func defaultMounts() []Mount {
	return []Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755"},
		},
		{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"},
		},
	}
}
