// Package spec — OCI Runtime Spec dəstəyi.
//
// Mənbə: https://github.com/opencontainers/runtime-spec
//
// Bu paket runc/Docker-in istifadə etdiyi config.json formatı ilə uyğun
// gəlir. Hər container üçün bir config.json var, içində:
//   - process (cmd, env, capabilities)
//   - root (rootfs path, readonly)
//   - mounts (lazımi mount-lar)
//   - linux (namespaces, cgroups, seccomp, ...)
package spec

// Spec — OCI Runtime Spec kök obyekti.
//
// Bu struct config.json-a uyğundur. Bütün sahələri istifadə etmirik,
// amma standartla uyğunluq üçün eyni adlar saxlanılır.
type Spec struct {
	OCIVersion string   `json:"ociVersion"`        // "1.0.2"
	Process    *Process `json:"process,omitempty"` // proses parametrləri
	Root       *Root    `json:"root,omitempty"`    // rootfs
	Hostname   string   `json:"hostname,omitempty"`
	Mounts     []Mount  `json:"mounts,omitempty"`
	Linux      *Linux   `json:"linux,omitempty"`
}

// Process — container daxilində işləyən prosesin parametrləri.
type Process struct {
	Terminal        bool          `json:"terminal,omitempty"`
	User            User          `json:"user"`
	Args            []string      `json:"args"`
	Env             []string      `json:"env,omitempty"`
	Cwd             string        `json:"cwd"`
	Capabilities    *Capabilities `json:"capabilities,omitempty"`
	NoNewPrivileges bool          `json:"noNewPrivileges,omitempty"`
}

type User struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}

// Capabilities — saxlanılan capability-lər (set-lər üzrə).
type Capabilities struct {
	Bounding    []string `json:"bounding,omitempty"`
	Effective   []string `json:"effective,omitempty"`
	Inheritable []string `json:"inheritable,omitempty"`
	Permitted   []string `json:"permitted,omitempty"`
	Ambient     []string `json:"ambient,omitempty"`
}

type Root struct {
	Path     string `json:"path"`               // rootfs path
	Readonly bool   `json:"readonly,omitempty"` // / read-only?
}

type Mount struct {
	Destination string   `json:"destination"`
	Source      string   `json:"source,omitempty"`
	Type        string   `json:"type,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Linux — Linux-spesifik parametrlər.
type Linux struct {
	Namespaces  []Namespace `json:"namespaces,omitempty"`
	Resources   *Resources  `json:"resources,omitempty"`
	CgroupsPath string      `json:"cgroupsPath,omitempty"`
}

type Namespace struct {
	Type string `json:"type"`           // "pid", "network", "mount", ...
	Path string `json:"path,omitempty"` // mövcud ns-ə qoşulmaq üçün
}

// Resources — cgroup limit-ləri.
type Resources struct {
	Memory *Memory `json:"memory,omitempty"`
	CPU    *CPU    `json:"cpu,omitempty"`
	Pids   *Pids   `json:"pids,omitempty"`
}

type Memory struct {
	Limit *int64 `json:"limit,omitempty"` // byte
}

type CPU struct {
	Quota  *int64  `json:"quota,omitempty"`  // microsecond / period
	Period *uint64 `json:"period,omitempty"` // microsecond
}

type Pids struct {
	Limit int64 `json:"limit"` // max PID sayı
}

// Sabit namespace tipləri (OCI standartı).
const (
	NamespacePID     = "pid"
	NamespaceNetwork = "network"
	NamespaceMount   = "mount"
	NamespaceIPC     = "ipc"
	NamespaceUTS     = "uts"
	NamespaceCgroup  = "cgroup"
	NamespaceUser    = "user"
)
