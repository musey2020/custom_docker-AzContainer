# azcontainer

Go dilində sıfırdan yazılmış Docker tipli container runtime. Linux namespaces, cgroups v2, OverlayFS, capabilities, seccomp, OCI image və runtime spesifikasiyaları, bridge networking + NAT, və JSON-RPC API ilə daemon arxitekturası daxildir.

> **Tədris layihəsi.** ~4300 sətir Go kod, heç bir xarici asılılıq yoxdur (yalnız Go standard library).

---

## Mündəricat

- [Bu nədir?](#bu-nədir)
- [Xüsusiyyətlər](#xüsusiyyətlər)
- [Arxitektura](#arxitektura)
- [Quraşdırma](#quraşdırma)
- [Sürətli başlanğıc](#sürətli-başlanğıc)
- [İnkişaf mərhələləri](#inkişaf-mərhələləri)
    - [Mərhələ 0: Linux primitivləri](#mərhələ-0-linux-primitivləri)
    - [Mərhələ 1: Namespaces](#mərhələ-1-namespaces)
    - [Mərhələ 2: Filesystem (chroot, pivot_root, OverlayFS)](#mərhələ-2-filesystem)
    - [Mərhələ 3: Cgroups v2](#mərhələ-3-cgroups-v2)
    - [Mərhələ 4: Təhlükəsizlik (capabilities, seccomp, no_new_privs)](#mərhələ-4-təhlükəsizlik)
    - [Mərhələ 5: OCI Image Spec](#mərhələ-5-oci-image-spec)
    - [Mərhələ 6: OCI Runtime Spec](#mərhələ-6-oci-runtime-spec)
    - [Mərhələ 7: Networking (veth, bridge, NAT)](#mərhələ-7-networking)
    - [Mərhələ 8: Daemon + CLI ayrılığı](#mərhələ-8-daemon--cli-ayrılığı)
    - [Mərhələ 9: Polish (logging, metrics)](#mərhələ-9-polish)
- [Layihə strukturu](#layihə-strukturu)
- [Məhdudiyyətlər](#məhdudiyyətlər)
- [Mənbələr](#mənbələr)

---

## Bu nədir?

`azcontainer` Docker-in arxa planda necə işlədiyini göstərən minimal container runtime-dır. 10 mərhələdə tədricən qurulub — hər mərhələ həqiqi container runtime-in bir əsas qabiliyyətini əlavə edir.

Məqsəd **quraraq öyrənmək**-dir, Docker-i production-da əvəz etmək deyil. Sonunda anlayacaqsan:

- Linux namespaces proseslərı necə izolyasiya edir
- OverlayFS copy-on-write filesystem-i necə təmin edir
- Cgroups v2 resurs limitlərini necə tətbiq edir
- Capabilities və seccomp privilegiyaları necə məhdudlaşdırır
- OCI image və runtime spesifikasiyaları necə işləyir
- Container şəbəkəsi `veth` cütlüklərindən və bridge-dən necə qurulur
- Docker-in client-daemon arxitekturası necə işləyir

---

## Xüsusiyyətlər

- **Linux namespaces:** PID, network, mount, IPC, UTS, cgroup
- **OverlayFS rootfs** hər container üçün copy-on-write
- **cgroups v2** CPU, yaddaş və PID limit-ləri üçün
- **Təhlükəsizlik:** no_new_privs, capability drop, seccomp BPF filter (~30 təhlükəli syscall bloklanır)
- **OCI Image Spec v1:** Docker Hub-dan pull, manifest parse, layer assembly (whiteout dəstəyi)
- **OCI Runtime Spec v1:** `config.json` generasiya və icra
- **Networking:** bridge interface, veth cütlükləri, IP allocation, iptables MASQUERADE
- **Daemon arxitekturası:** Unix socket üzərindən JSON-RPC, detached container-lər, state persistence
- **Müşahidə:** strukturlaşdırılmış logging, Prometheus `/metrics` endpoint, `stats --watch`
- **Sıfır asılılıq:** yalnız Go standard library

---

## Arxitektura

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
                                    │ (namespaces,│   │ (bridge,│   │ (pull,   │
                                    │  cgroups,│   │  veth,   │   │  layers, │
                                    │  exec)   │   │  NAT)    │   │  assemble)│
                                    └──────────┘   └──────────┘   └──────────┘

                  ┌─────────────────────────────────────┐
                  │  /var/lib/azcontainer/              │
                  │  ├── images/<ad>/{manifest,config,rootfs}
                  │  ├── blobs/sha256/<hash>            │
                  │  ├── containers/<id>/{upper,work,merged}
                  │  ├── state/<id>.json                │
                  │  └── logs/<id>.log                  │
                  └─────────────────────────────────────┘
```

---

## Quraşdırma

### Tələblər

- Linux kernel 5.x+ (cgroups v2 məcburidir)
- Go 1.22+
- Root icazəsi (container-lər namespace, mount və s. tələb edir)
- `iptables`, `ip`, `nsenter` (adətən artıq qurulub)

### Build

```bash
git clone https://github.com/<istifadəçi-adın>/azcontainer.git
cd azcontainer
go build -o azcontainer ./cmd/azcontainer
```

---

## Sürətli başlanğıc

### 1. Image hazırla

Mövcud Alpine rootfs-in varsa:

```bash
# Hər hansı mövcud rootfs-dən
sudo tar -czf /tmp/alpine-rootfs.tar.gz -C /path/to/alpine-rootfs .
sudo ./azcontainer import-rootfs /tmp/alpine-rootfs.tar.gz alpine
```

Və ya Docker Hub-dan pull et:

```bash
sudo ./azcontainer pull alpine:latest
```

### 2. Container işə sal (direct rejimi)

```bash
sudo ./azcontainer run-direct alpine /bin/sh
```

### 3. Və ya daemon istifadə et

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

## İnkişaf mərhələləri

Hər mərhələ əvvəlkinin üstündə qurulur. Sıfırdan işləyən container runtime-a qədər tam yol.

### Mərhələ 0: Linux primitivləri

**Məqsəd:** Kod yazmazdan əvvəl tikinti bloklarını anlamaq.

Go-ya toxunmazdan əvvəl istifadə edəcəyimiz Linux primitivləri ilə tanış ol:

```bash
# Namespaces — izolyasiyalı PID namespace yarat
sudo unshare --pid --fork --mount-proc /bin/sh

# Mount — qovluğa chroot et
sudo chroot /path/to/rootfs /bin/sh

# Cgroups — resurs məhdud qrup yarat
sudo mkdir /sys/fs/cgroup/test
echo 100000 > /sys/fs/cgroup/test/cpu.max
```

**Niyə vacibdir:** Hər container runtime əslində bu syscall-ların sarğısıdır. Onları əl ilə bilmək kodu aydın edir.

### Mərhələ 1: Namespaces

**Fayllar:** `internal/runtime/runtime.go` (ilk versiya)

**Nə edir:** `syscall.SysProcAttr.Cloneflags` vasitəsilə `clone(2)` istifadə edərək yeni namespace-lərlə child proses yaradır:

- `CLONE_NEWPID` — yeni PID namespace (container PID 1-dir)
- `CLONE_NEWNS` — yeni mount namespace
- `CLONE_NEWUTS` — yeni hostname
- `CLONE_NEWIPC` — yeni IPC namespace
- `CLONE_NEWNET` — yeni network namespace (izolyasiyalı, hələ interface yox)

**Əsas trick:** `/proc/self/exe` re-execution pattern. Parent özü-özünü `init` subkomandası ilə çağırır. Child yeni namespace-lərdə başlayır və container-i quraşdırır.

```go
cmd := exec.Command("/proc/self/exe", "init", containerID, ...)
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | ...,
}
```

### Mərhələ 2: Filesystem

**Fayllar:** `internal/filesystem/{overlay.go,pivot.go}`

**Nə edir:**

1. **OverlayFS layout:** Hər container öz `upper/`, `work/`, `merged/` qovluqlarına sahibdir. Image rootfs read-only `lower`-dir. Container daxilindən yazılar `upper/`-ə gedir (copy-on-write).

2. **pivot_root:** Proses-in root filesystem-ini merged OverlayFS mount-u ilə əvəz edir. `pivot_root`-dan sonra host filesystem-i tamamilə əlçatmazdır.

3. **Virtual filesystem-lər:** Container daxilində `/proc`, `/sys`, `/dev` mount edir.

**Kritik addım:** `pivot_root`-dan əvvəl mount propagation-u `MS_PRIVATE` təyin etmək, əks halda mount-lar host-a sızır.

### Mərhələ 3: Cgroups v2

**Fayllar:** `internal/cgroup/cgroup.go`

**Nə edir:** `/sys/fs/cgroup/azcontainer/<id>/`-də cgroup yaradır və resurs limitlərini tətbiq edir:

- `cpu.max` — CPU quota (məsələn, `50000 100000` = bir core-un 50%-i)
- `memory.max` — bayt-da RAM limiti
- `pids.max` — maksimum proses sayı

**Diqqət:** Child cgroup-lar onları istifadə edə bilməzdən əvvəl controller-lər parent cgroup-larda `cgroup.subtree_control` vasitəsilə aktivləşdirilməlidir.

### Mərhələ 4: Təhlükəsizlik

**Fayllar:** `internal/security/{security.go,capabilities.go,seccomp.go}`

Ardıcıllıqla əlavə edilən üç müdafiə qatı:

**4a — `no_new_privs`:** setuid binary-lər vasitəsilə privilege escalation-ın qarşısını alır. Bir dəfə təyin edildikdən sonra geri qaytarıla bilməz.
```c
prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
```

**4b — Capability drop:** Bounding set-dən ~25 təhlükəli capability-ni silir (məsələn, `CAP_SYS_ADMIN`, `CAP_SYS_MODULE`, `CAP_NET_ADMIN`). Docker-default minimal seti saxlayır (~13 cap).

**4c — Seccomp BPF filter:** Əl ilə yazılmış BPF bytecode ~30 təhlükəli syscall-ı (`reboot`, `keyctl`, `kexec_load`, `bpf`, `userfaultfd` və s.) `ENOSYS` qaytararaq bloklayır. `libseccomp` asılılığı yoxdur.

### Mərhələ 5: OCI Image Spec

**Fayllar:** `internal/image/{types.go,store.go,import.go,pull.go,assemble.go}`

Tam OCI Image Spec v1 dəstəyi:

- **Content-addressable storage:** Blob-lar SHA256 hash-ə görə `/var/lib/azcontainer/blobs/sha256/`-də saxlanılır. Paylaşılan layer-lər dublikasiya edilmir.
- **Manifest parsing:** Həm Docker v2, həm OCI manifest formatları dəstəklənir.
- **Registry client:** Docker Hub-dan pull edir (anonim auth + bearer token).
- **Layer assembly:** Layer-ləri ardıcıllıqla extract edir, whiteout fayllarını emal edir (`.wh.X`, `.wh..wh..opq`).
- **Bir neçə import yolu:** Docker `save` tar, sadə rootfs tar.gz, və ya registry-dən pull.

### Mərhələ 6: OCI Runtime Spec

**Fayllar:** `internal/spec/{spec.go,generate.go,load.go}`

Standart `config.json` dəstəyi — `runc`-un istifadə etdiyi eyni format:

```bash
azcontainer spec alpine > config.json   # default generasiya
vim config.json                          # limit-ləri, capabilities-i və s. dəyiş
azcontainer run --spec config.json       # custom config ilə işə sal
```

OCI spec sahələrini bunlara map edir:
- Namespace tipləri → `CLONE_NEW*` flag-lər
- `linux.resources` → cgroup config
- `process.capabilities` → security setup
- `mounts` → mount syscall-ları

### Mərhələ 7: Networking

**Fayllar:** `internal/network/{bridge.go,veth.go,ipam.go,nat.go,setup.go}`

End-to-end container şəbəkəsi:

```
HOST                                  CONTAINER
─────────────────                     ─────────────
azcontainer0  10.88.0.1/16 (bridge)
     │
     ├── veth-A ─────── veth-B ────── eth0  10.88.0.2/16
     │   (host tərəfi)                (container ns-də
     │                                 eth0 adı verilir)
     │
     └── iptables MASQUERADE          default via 10.88.0.1
         → internetə çıxış
```

**İmplementasiya:**
1. Bridge bir dəfə yaranır (`azcontainer0`)
2. Hər container üçün: IP allocate, veth cütlüyü yarat, host tərəfini bridge-ə qoş
3. Container tərəfini `ip link set ... netns <pid>` vasitəsilə container-in network namespace-inə köçür
4. Namespace daxilində `nsenter -t <pid> -n -- ip ...` ilə konfiqurasiya et
5. IP forwarding aktivləşdir (`net.ipv4.ip_forward=1`)
6. Çıxan trafik üçün iptables `MASQUERADE` qaydası əlavə et

**Sinxronizasiya:** Parent və child arasında pipe təmin edir ki, container şəbəkə interface hazır olmazdan əvvəl `exec` etməsin.

### Mərhələ 8: Daemon + CLI ayrılığı

**Fayllar:** `internal/daemon/{api.go,server.go,client.go}`, `internal/state/`, `internal/runtime/detached.go`

Monolit CLI-ı Docker-tipli client/daemon arxitekturasına refactor:

- **Unix socket üzərindən JSON-RPC** — `/var/run/azcontainer.sock` (gRPC əvəzinə — internet asılılığı yoxdur)
- **Detached container-lər:** daemon container-ləri yaradır və goroutine-lərdə izləyir
- **State persistence:** hər container-in `/var/lib/azcontainer/state/<id>.json`-u var
- **Prefix matching:** `azcontainer stop abc12` Docker kimi `abc123def456...`-ı tapır

Yeni komandalar: `run` (artıq detached), `ps`, `stop`, `rm`, `logs`.

### Mərhələ 9: Polish

**Fayllar:** `internal/log/`, `internal/errors/`, `internal/metrics/`, `internal/daemon/stats.go`

Production keyfiyyəti üçün toxunuşlar:

- **Strukturlaşdırılmış logging** level-lər və JSON format opsiyası ilə (`AZC_LOG_LEVEL=DEBUG AZC_LOG_FORMAT=json`)
- **Typed errors** `errors.Is()` dəstəyi ilə (`ErrNotFound`, `ErrInvalidInput` və s.)
- **Per-container metric-lər** cgroup fayllarından (CPU nanosaniyə, yaddaş bayt, PID)
- **Prometheus endpoint** — `http://localhost:9090/metrics`
- **`stats --watch`** komandası real-time resurs monitorinqi üçün
- **Panic recovery** RPC handler-lərində ki, daemon bug-larda sağ qalsın

---

## Layihə strukturu

```
azcontainer/
├── cmd/azcontainer/main.go      # CLI giriş nöqtəsi
├── internal/
│   ├── cgroup/                   # Mərhələ 3 — cgroups v2
│   ├── filesystem/               # Mərhələ 2 — overlay + pivot_root
│   ├── security/                 # Mərhələ 4 — no_new_privs, caps, seccomp
│   ├── image/                    # Mərhələ 5 — OCI image spec
│   ├── spec/                     # Mərhələ 6 — OCI runtime spec
│   ├── network/                  # Mərhələ 7 — bridge + veth + NAT
│   ├── state/                    # Mərhələ 8 — container state persistence
│   ├── daemon/                   # Mərhələ 8 — JSON-RPC server/client
│   ├── runtime/                  # Mərhələ 1, 8 — container lifecycle
│   ├── log/                      # Mərhələ 9 — strukturlaşdırılmış logging
│   ├── errors/                   # Mərhələ 9 — typed errors
│   └── metrics/                  # Mərhələ 9 — Prometheus metrics
└── go.mod
```

---

## Məhdudiyyətlər

Bu tədris layihəsidir. Bunlar YOXDUR:

- **TTY/interactive rejimi** — `docker run -it` yoxdur. PTY allocation implementasiya olunmayıb.
- **`docker exec` ekvivalenti** — işləyən container-ə bağlanmaq olmur.
- **Image build** — `Dockerfile` parser yoxdur.
- **Image push** — yalnız pull var.
- **User namespaces** — rootless container-lər dəstəklənmir.
- **Volume mount-lar** — bind mount və ya named volume yoxdur.
- **Multi-arch image-lər** — yalnız x86_64 fərz edilir.
- **Production təhlükəsizliyi** — RPC socket-də auth yoxdur, container-lər arası resurs quota-ları yoxdur.

Production üçün Docker, containerd, podman və ya runc istifadə et.

---

## Mənbələr

- [Linux man pages: `namespaces(7)`, `cgroups(7)`, `capabilities(7)`, `seccomp(2)`](https://man7.org/linux/man-pages/)
- [OCI Image Spec](https://github.com/opencontainers/image-spec)
- [OCI Runtime Spec](https://github.com/opencontainers/runtime-spec)
- [Linux Container in 500 lines (Lizzie Dixon)](https://blog.lizzie.io/linux-containers-in-500-loc.html)
- [Containers from scratch (Liz Rice)](https://www.youtube.com/watch?v=8fi7uSYlOdc)
- [Docker source code](https://github.com/moby/moby)

---

## Lisenziya

MIT