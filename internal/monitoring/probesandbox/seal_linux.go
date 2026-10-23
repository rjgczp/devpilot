//go:build schedulerfixture && linux

// Package probesandbox seals offline and independently default-denied fixture
// processes. It grants no destination permission or administrative lifecycle.
package probesandbox

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

var ErrBoundary = errors.New("probe boundary unavailable")

type Proof struct {
	InheritedFDsOnly   bool   `json:"inherited_fds_only"`
	UID                int    `json:"uid"`
	GID                int    `json:"gid"`
	CapabilitiesZero   bool   `json:"capabilities_zero"`
	NoNewPrivileges    bool   `json:"no_new_privileges"`
	SeccompThreads     bool   `json:"seccomp_all_threads"`
	MemoryMax          int64  `json:"memory_max"`
	CPUQuota           int64  `json:"cpu_quota"`
	CPUPeriod          int64  `json:"cpu_period"`
	PIDsMax            int64  `json:"pids_max"`
	FDLimit            uint64 `json:"fd_limit"`
	FDExhaustion       bool   `json:"fd_exhaustion"`
	RootReadOnly       bool   `json:"root_read_only"`
	EnvironmentClean   bool   `json:"environment_clean"`
	SecretsUnavailable bool   `json:"secrets_unavailable"`
	AdminDenied        bool   `json:"admin_denied"`
	SocketsDenied      bool   `json:"sockets_denied"`
	TCPOnly            bool   `json:"tcp_only"`
}

func readNumber(p string) (int64, error) {
	b, e := os.ReadFile(p)
	if e != nil {
		return 0, ErrBoundary
	}
	n, e := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if e != nil {
		return 0, ErrBoundary
	}
	return n, nil
}
func statusOK(path string) bool {
	b, e := os.ReadFile(path)
	if e != nil {
		return false
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			fields[k] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"CapInh", "CapPrm", "CapEff", "CapAmb", "CapBnd"} {
		if fields[k] != "0000000000000000" {
			return false
		}
	}
	return fields["NoNewPrivs"] == "1" && fields["Seccomp"] == "2"
}
func Seal() (Proof, error) { return seal(false) }

// SealNetwork is only for the independently default-denied fixture namespace.
// It permits TCP sockets, never UDP/raw/UNIX or administration. Seal stays offline.
func SealNetwork() (Proof, error) { return seal(true) }

func seal(network bool) (Proof, error) {
	// Enforce fixed inherited identity; never silently downgrade a root Probe.
	if os.Getuid() != 65534 || os.Geteuid() != 65534 || os.Getgid() != 65534 {
		return Proof{}, ErrBoundary
	}
	groups, e := os.Getgroups()
	if e != nil || len(groups) != 0 {
		return Proof{}, ErrBoundary
	}
	for _, v := range os.Environ() {
		if v != "LANG=C" && v != "TZ=UTC" {
			return Proof{}, ErrBoundary
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 256, Max: 256}) != nil || unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}) != nil || unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{}) != nil || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return Proof{}, ErrBoundary
	}
	arch := uint32(0)
	switch runtime.GOARCH {
	case "arm64":
		arch = unix.AUDIT_ARCH_AARCH64
	case "amd64":
		arch = unix.AUDIT_ARCH_X86_64
	default:
		return Proof{}, ErrBoundary
	}
	filter := []unix.SockFilter{{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4}, {Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: arch, Jt: 1}, {Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS}, {Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}}
	filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, K: 0x40000000, Jf: 1}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)})
	// The offline default denies all sockets/connect. The separately selected
	// network fixture restricts creation to TCP; independent rules grant targets.
	denied := []uint32{unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV, unix.SYS_SOCKETPAIR, unix.SYS_BIND, unix.SYS_LISTEN, unix.SYS_ACCEPT, unix.SYS_ACCEPT4, unix.SYS_SENDTO, unix.SYS_SENDMSG, unix.SYS_RECVFROM, unix.SYS_RECVMSG, unix.SYS_PTRACE, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_SETNS, unix.SYS_UNSHARE, unix.SYS_BPF, unix.SYS_EXECVE, unix.SYS_EXECVEAT, unix.SYS_CLONE3}
	if !network {
		denied = append(denied, unix.SYS_SOCKET, unix.SYS_CONNECT)
	} else {
		// A socket syscall can only create AF_INET/AF_INET6 SOCK_STREAM TCP.
		// Reload syscall number after argument inspection for subsequent rules.
		filter = append(filter,
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_SOCKET, Jf: 11},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_INET, Jt: 2},
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_INET6, Jt: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24},
			unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: ^uint32(unix.SOCK_CLOEXEC | unix.SOCK_NONBLOCK)},
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SOCK_STREAM, Jf: 3},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 32},
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 0, Jt: 2},
			unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.IPPROTO_TCP, Jt: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
			unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0})
	}
	for _, nr := range denied {
		filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: nr, Jf: 1}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)})
	}
	// clone is retained only for runtime threads, never process/namespace creation.
	filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_CLONE, Jf: 4}, unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16}, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: unix.CLONE_THREAD, Jt: 1}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program))); errno != 0 || result != 0 {
		return Proof{}, ErrBoundary
	}
	fdsObserved, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return Proof{}, ErrBoundary
	}
	for _, entry := range fdsObserved {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			return Proof{}, ErrBoundary
		}
		if fd <= 4 || network && fd <= 6 {
			continue
		}
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || (target != "anon_inode:[eventpoll]" && target != "anon_inode:[eventfd]") {
			return Proof{}, ErrBoundary
		}
	}
	p := Proof{InheritedFDsOnly: true, UID: os.Getuid(), GID: os.Getgid(), CapabilitiesZero: true, NoNewPrivileges: true, SeccompThreads: true, EnvironmentClean: true, SecretsUnavailable: true}
	tasks, e := os.ReadDir("/proc/self/task")
	if e != nil {
		return Proof{}, ErrBoundary
	}
	for _, t := range tasks {
		if !statusOK("/proc/self/task/" + t.Name() + "/status") {
			return Proof{}, ErrBoundary
		}
	}
	var limit unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_NOFILE, &limit) != nil || limit.Cur != 256 || limit.Max != 256 {
		return Proof{}, ErrBoundary
	}
	p.FDLimit = limit.Cur
	p.MemoryMax, e = readNumber("/sys/fs/cgroup/memory.max")
	if e != nil || p.MemoryMax != 128*1024*1024 {
		return Proof{}, ErrBoundary
	}
	p.PIDsMax, e = readNumber("/sys/fs/cgroup/pids.max")
	if e != nil || p.PIDsMax != 128 {
		return Proof{}, ErrBoundary
	}
	b, e := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if e != nil {
		return Proof{}, ErrBoundary
	}
	parts := strings.Fields(string(b))
	if len(parts) != 2 {
		return Proof{}, ErrBoundary
	}
	p.CPUQuota, e = strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return Proof{}, ErrBoundary
	}
	p.CPUPeriod, e = strconv.ParseInt(parts[1], 10, 64)
	if e != nil || p.CPUQuota <= 0 || p.CPUQuota != p.CPUPeriod {
		return Proof{}, ErrBoundary
	}
	var fs unix.Statfs_t
	if unix.Statfs("/", &fs) != nil || fs.Flags&unix.ST_RDONLY == 0 {
		return Proof{}, ErrBoundary
	}
	p.RootReadOnly = true
	for _, path := range []string{"/var/run/docker.sock", "/run/containerd/containerd.sock", "/root/.aws/credentials", "/root/.kube/config", "/app/.env", "/workspace"} {
		if _, err := os.Open(path); err == nil {
			return Proof{}, ErrBoundary
		}
	}
	for _, family := range []int{unix.AF_INET, unix.AF_INET6, unix.AF_UNIX} {
		typ := unix.SOCK_STREAM
		if network && family != unix.AF_UNIX {
			typ = unix.SOCK_DGRAM
		}
		fd, err := unix.Socket(family, typ, 0)
		if err != unix.EPERM {
			if fd >= 0 {
				_ = unix.Close(fd)
			}
			return Proof{}, ErrBoundary
		}
	}
	p.SocketsDenied = !network
	p.TCPOnly = network
	if network {
		for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
			fd, err := unix.Socket(family, unix.SOCK_STREAM, unix.IPPROTO_TCP)
			if err != nil {
				return Proof{}, ErrBoundary
			}
			_ = unix.Close(fd)
			fd, err = unix.Socket(family, unix.SOCK_RAW, unix.IPPROTO_RAW)
			if fd >= 0 {
				_ = unix.Close(fd)
			}
			if err != unix.EPERM {
				return Proof{}, ErrBoundary
			}
		}
	}
	if unix.Unshare(unix.CLONE_NEWNET) != unix.EPERM {
		return Proof{}, ErrBoundary
	}
	p.AdminDenied = true
	// Real descriptor exhaustion observes EMFILE at the inherited256 boundary.
	var fds []int
	for {
		fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err == unix.EMFILE {
			p.FDExhaustion = true
			break
		}
		if err != nil {
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
			return Proof{}, ErrBoundary
		}
		fds = append(fds, fd)
		if len(fds) > 256 {
			return Proof{}, ErrBoundary
		}
	}
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
	return p, nil
}
