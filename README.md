# Azcontainer

A Docker-like container runtime built from scratch in Go. Implements Linux namespaces, cgroups v2, OverlayFS, capabilities, seccomp, OCI image & runtime specs, bridge networking with NAT, and a daemon with JSON-RPC API.

> **Educational project.** ~4300 lines of Go, zero external dependencies (only Go standard library).

---

## Table of Contents

- [What is this?](#what-is-this)
- [Features](#features)
- [Architecture](#architecture)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Build Stages](#build-stages)
    - [Stage 0: Linux Primitives](#stage-0-linux-primitives)
    - [Stage 1: Namespaces](#stage-1-namespaces)
    - [Stage 2: Filesystem (chroot, pivot_root, OverlayFS)](#stage-2-filesystem)
    - [Stage 3: Cgroups v2](#stage-3-cgroups-v2)
    - [Stage 4: Security (capabilities, seccomp, no_new_privs)](#stage-4-security)
    - [Stage 5: OCI Image Spec](#stage-5-oci-image-spec)
    - [Stage 6: OCI Runtime Spec](#stage-6-oci-runtime-spec)
    - [Stage 7: Networking (veth, bridge, NAT)](#stage-7-networking)
    - [Stage 8: Daemon + CLI Split](#stage-8-daemon--cli-split)
    - [Stage 9: Polish (logging, metrics)](#stage-9-polish)
- [Project Structure](#project-structure)
- [Limitations](#limitations)
- [References](#references)

---

## What is this?

`azcontainer` is a minimal container runtime that demonstrates how Docker actually works under the hood. It's built incrementally across 10 stages — each stage adds one core capability of a real container runtime.

The goal is **learning by building**, not replacing Docker in production. By the end, you'll understand:

- How Linux namespaces isolate processes
- How OverlayFS provides copy-on-write filesystems
- How cgroups v2 enforce resource limits
- How capabilities and seccomp restrict privileges
- How OCI image and runtime specifications work
- How container networking is built from `veth` pairs and bridges
- How Docker's client-daemon architecture works

---

## Features

- **Linux namespaces:** PID, network, mount, IPC, UTS, cgroup
- **OverlayFS rootfs** with copy-on-write per container
- **cgroups v2** for CPU, memory, and PID limits
- **Security:** no_new_privs, capability dropping, seccomp BPF filter (~30 dangerous syscalls blocked)
- **OCI Image Spec v1:** pull from Docker Hub, parse manifests, assemble layers (whiteout support)
- **OCI Runtime Spec v1:** `config.json` generation and execution
- **Networking:** bridge interface, veth pairs, IP allocation, iptables MASQUERADE
- **Daemon architecture:** JSON-RPC over Unix socket, detached containers, state persistence
- **Observability:** structured logging, Prometheus `/metrics` endpoint, `stats --watch`
- **Zero dependencies:** uses only Go standard library

---

## Architecture

```
┌─────────────────┐         JSON-RPC          ┌─────────────────────┐
│  azcontainer    │  ─────────────────────▶   │  azcontainer daemon │
│     (CLI)       │   /var/run/azcontainer    │                     │
└─────────────────┘                            └──────────┬──────────┘
                                                          │
                                          ┌───────────────┼───────────────┐
                                          ▼               ▼               ▼
                                    ┌──────────┐   ┌──────────┐   ┌──────────┐
                                    │ runtime  │   │ network  │   │  image   │
                                    │ (namespaces, │   │ (bridge, │   │ (pull,   │
                                    │  cgroups,│   │  veth,   │   │  layers, │
                                    │  exec)   │   │  NAT)    │   │  assemble)│
                                    └──────────┘   └──────────┘   └──────────┘

                  ┌─────────────────────────────────────┐
                  │  /var/lib/azcontainer/              │
                  │  ├── images/<name>/{manifest,config,rootfs}
                  │  ├── blobs/sha256/<hash>            │
                  │  ├── containers/<id>/{upper,work,merged}
                  │  ├── state/<id>.json                │
                  │  └── logs/<id>.log                  │
                  └─────────────────────────────────────┘
```

---

## Installation

### Requirements

- Linux kernel 5.x+ (cgroups v2 required)
- Go 1.22+
- Root privileges (containers require namespaces, mounts, etc.)
- `iptables`, `ip`, `nsenter` (usually pre-installed)

### Build

```bash
git clone https://github.com/<your-username>/azcontainer.git
cd azcontainer
go build -o azcontainer ./cmd/azcontainer
```

---

## Quick Start

### 1. Prepare an image

If you have an existing Alpine rootfs:

```bash
# From any existing rootfs
sudo tar -czf /tmp/alpine-rootfs.tar.gz -C /path/to/alpine-rootfs .
sudo ./azcontainer import-rootfs /tmp/alpine-rootfs.tar.gz alpine
```

Or pull from Docker Hub:

```bash
sudo ./azcontainer pull alpine:latest
```

### 2. Run a container (direct mode)

```bash
sudo ./azcontainer run-direct alpine /bin/sh
```

### 3. Or use the daemon

**Terminal 1:**
```bash
sudo ./azcontainer daemon
```

**Terminal 2:**
```bash
./azcontainer run alpine /bin/sleep 100
./azcontainer ps
./azcontainer stats <id> --watch
./azcontainer logs <id>
./azcontainer stop <id>
./azcontainer rm <id>
```

---

## Build Stages

Each stage builds on the previous one. The full journey from zero to a working container runtime.

### Stage 0: Linux Primitives

**Goal:** Understand the building blocks before writing any code.

Before touching Go, get comfortable with the Linux primitives we'll use:

```bash
# Namespaces — create an isolated PID namespace
sudo unshare --pid --fork --mount-proc /bin/sh

# Mount — chroot into a directory
sudo chroot /path/to/rootfs /bin/sh

# Cgroups — create a resource-limited group
sudo mkdir /sys/fs/cgroup/test
echo 100000 > /sys/fs/cgroup/test/cpu.max
```

**Why this matters:** Every container runtime is just a wrapper around these syscalls. Knowing them by hand makes the code obvious.

### Stage 1: Namespaces

**Files:** `internal/runtime/runtime.go` (initial version)

**What it does:** Spawns a child process with new namespaces using `clone(2)` via `syscall.SysProcAttr.Cloneflags`:

- `CLONE_NEWPID` — new PID namespace (container is PID 1)
- `CLONE_NEWNS` — new mount namespace
- `CLONE_NEWUTS` — new hostname
- `CLONE_NEWIPC` — new IPC namespace
- `CLONE_NEWNET` — new network namespace (isolated, no interfaces yet)

**Key trick:** `/proc/self/exe` re-execution pattern. The parent calls itself with `init` subcommand. The child starts in the new namespaces and sets up the container.

```go
cmd := exec.Command("/proc/self/exe", "init", containerID, ...)
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | ...,
}
```

### Stage 2: Filesystem

**Files:** `internal/filesystem/{overlay.go,pivot.go}`

**What it does:**

1. **OverlayFS layout:** Each container gets its own `upper/`, `work/`, `merged/` directories. The image rootfs is the read-only `lower`. Writes from inside the container go to `upper/` (copy-on-write).

2. **pivot_root:** Replaces the process root filesystem with the merged OverlayFS mount. After `pivot_root`, the host filesystem is completely inaccessible.

3. **Virtual filesystems:** Mounts `/proc`, `/sys`, `/dev` inside the container.

**Critical step:** Setting mount propagation to `MS_PRIVATE` before `pivot_root`, otherwise mounts leak to the host.

### Stage 3: Cgroups v2

**Files:** `internal/cgroup/cgroup.go`

**What it does:** Creates a cgroup at `/sys/fs/cgroup/azcontainer/<id>/` and applies resource limits:

- `cpu.max` — CPU quota (e.g., `50000 100000` = 50% of one core)
- `memory.max` — RAM limit in bytes
- `pids.max` — maximum process count

**Gotcha:** Controllers must be enabled on parent cgroups via `cgroup.subtree_control` before child cgroups can use them.

### Stage 4: Security

**Files:** `internal/security/{security.go,capabilities.go,seccomp.go}`

Three defense layers added in order:

**4a — `no_new_privs`:** Prevents privilege escalation via setuid binaries. Once set, can never be unset.
```c
prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
```

**4b — Capability dropping:** Removes ~25 dangerous capabilities from the bounding set (e.g., `CAP_SYS_ADMIN`, `CAP_SYS_MODULE`, `CAP_NET_ADMIN`). Keeps Docker-default minimal set (~13 caps).

**4c — Seccomp BPF filter:** Hand-written BPF bytecode blocks ~30 dangerous syscalls (`reboot`, `keyctl`, `kexec_load`, `bpf`, `userfaultfd`, etc.) by returning `ENOSYS`. No `libseccomp` dependency.

### Stage 5: OCI Image Spec

**Files:** `internal/image/{types.go,store.go,import.go,pull.go,assemble.go}`

Full OCI Image Spec v1 support:

- **Content-addressable storage:** Blobs stored by SHA256 hash in `/var/lib/azcontainer/blobs/sha256/`. Shared layers are deduplicated.
- **Manifest parsing:** Both Docker v2 and OCI manifest formats supported.
- **Registry client:** Pulls from Docker Hub (anonymous auth + bearer token flow).
- **Layer assembly:** Extracts layers in order with whiteout file handling (`.wh.X`, `.wh..wh..opq`).
- **Multiple import paths:** Docker `save` tar, plain rootfs tar.gz, or pull from registry.

### Stage 6: OCI Runtime Spec

**Files:** `internal/spec/{spec.go,generate.go,load.go}`

Standard `config.json` support — the same format `runc` uses:

```bash
azcontainer spec alpine > config.json   # generate default
vim config.json                          # edit limits, capabilities, etc.
azcontainer run --spec config.json       # run with custom config
```

Maps OCI spec fields to:
- Namespace types → `CLONE_NEW*` flags
- `linux.resources` → cgroup config
- `process.capabilities` → security setup
- `mounts` → mount syscalls

### Stage 7: Networking

**Files:** `internal/network/{bridge.go,veth.go,ipam.go,nat.go,setup.go}`

End-to-end container networking:

```
HOST                                  CONTAINER
─────────────────                     ─────────────
azcontainer0  10.88.0.1/16 (bridge)
     │
     ├── veth-A ─────── veth-B ────── eth0  10.88.0.2/16
     │   (host side)                  (renamed to eth0
     │                                 inside container ns)
     │
     └── iptables MASQUERADE          default via 10.88.0.1
         → outbound internet
```

**Implementation:**
1. Bridge created once (`azcontainer0`)
2. Per container: allocate IP, create veth pair, attach host side to bridge
3. Move container side to container's network namespace via `ip link set ... netns <pid>`
4. Configure inside namespace using `nsenter -t <pid> -n -- ip ...`
5. Enable IP forwarding (`net.ipv4.ip_forward=1`)
6. Add iptables `MASQUERADE` rule for outbound traffic

**Synchronization:** A pipe between parent and child ensures the container doesn't `exec` before the network interface is ready.

### Stage 8: Daemon + CLI Split

**Files:** `internal/daemon/{api.go,server.go,client.go}`, `internal/state/`, `internal/runtime/detached.go`

Refactored monolithic CLI into Docker-like client/daemon architecture:

- **JSON-RPC over Unix socket** at `/var/run/azcontainer.sock` (instead of gRPC — no internet dependency)
- **Detached containers:** daemon spawns containers and tracks them in goroutines
- **State persistence:** each container has `/var/lib/azcontainer/state/<id>.json`
- **Prefix matching:** `azcontainer stop abc12` finds `abc123def456...` like Docker

New commands: `run` (now detached), `ps`, `stop`, `rm`, `logs`.

### Stage 9: Polish

**Files:** `internal/log/`, `internal/errors/`, `internal/metrics/`, `internal/daemon/stats.go`

Production-quality touches:

- **Structured logging** with levels and JSON format option (`AZC_LOG_LEVEL=DEBUG AZC_LOG_FORMAT=json`)
- **Typed errors** with `errors.Is()` support (`ErrNotFound`, `ErrInvalidInput`, etc.)
- **Per-container metrics** from cgroup files (CPU nanos, memory bytes, PIDs)
- **Prometheus endpoint** at `http://localhost:9090/metrics`
- **`stats --watch`** command for real-time resource monitoring
- **Panic recovery** in RPC handlers so daemon survives bugs

---

## Project Structure

```
azcontainer/
├── cmd/azcontainer/main.go      # CLI entry point
├── internal/
│   ├── cgroup/                   # Stage 3 — cgroups v2
│   ├── filesystem/               # Stage 2 — overlay + pivot_root
│   ├── security/                 # Stage 4 — no_new_privs, caps, seccomp
│   ├── image/                    # Stage 5 — OCI image spec
│   ├── spec/                     # Stage 6 — OCI runtime spec
│   ├── network/                  # Stage 7 — bridge + veth + NAT
│   ├── state/                    # Stage 8 — container state persistence
│   ├── daemon/                   # Stage 8 — JSON-RPC server/client
│   ├── runtime/                  # Stages 1, 8 — container lifecycle
│   ├── log/                      # Stage 9 — structured logging
│   ├── errors/                   # Stage 9 — typed errors
│   └── metrics/                  # Stage 9 — Prometheus metrics
└── go.mod
```

---

## Limitations

This is an educational project. It does NOT have:

- **TTY/interactive mode** — no `docker run -it`. PTY allocation not implemented.
- **`docker exec` equivalent** — can't attach to running containers.
- **Image building** — no `Dockerfile` parser.
- **Image push** — pull only.
- **User namespaces** — rootless containers not supported.
- **Volume mounts** — no bind mounts or named volumes.
- **Multi-arch images** — assumes x86_64 only.
- **Production safety** — no auth on RPC socket, no resource quotas across containers.

For production, use Docker, containerd, podman, or runc.

---

## References

- [Linux man pages: `namespaces(7)`, `cgroups(7)`, `capabilities(7)`, `seccomp(2)`](https://man7.org/linux/man-pages/)
- [OCI Image Spec](https://github.com/opencontainers/image-spec)
- [OCI Runtime Spec](https://github.com/opencontainers/runtime-spec)
- [Linux Container in 500 lines (Lizzie Dixon)](https://blog.lizzie.io/linux-containers-in-500-loc.html)
- [Containers from scratch (Liz Rice)](https://www.youtube.com/watch?v=8fi7uSYlOdc)
- [Docker source code](https://github.com/moby/moby)

---

## License

MIT
