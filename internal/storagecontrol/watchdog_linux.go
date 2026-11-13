//go:build linux

package storagecontrol

import (
	"context"
	"encoding/binary"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxPulseAge = 3 * time.Second

func bootNow() (int64, error) {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &t) != nil {
		return 0, ErrReport
	}
	return t.Nano(), nil
}
func pulse() ([]byte, error) {
	n, e := bootNow()
	if e != nil {
		return nil, e
	}
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b, nil
}
func fresh(b []byte) bool {
	n, e := bootNow()
	if e != nil || len(b) != 8 {
		return false
	}
	stamp := int64(binary.BigEndian.Uint64(b))
	return stamp > 0 && stamp <= n && n-stamp < int64(maxPulseAge)
}

// InheritedPipe accepts anonymous IPC only, with the specified access mode.
func InheritedPipe(fd int, write bool) (*os.File, error) {
	mode := unix.O_RDONLY
	if write {
		mode = unix.O_WRONLY
	}
	var st unix.Stat_t
	flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	link, le := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	if e != nil || le != nil || flags&unix.O_ACCMODE != mode || unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFIFO || !strings.HasPrefix(link, "pipe:[") || unix.SetNonblock(fd, true) != nil {
		return nil, ErrReport
	}
	return os.NewFile(uintptr(fd), "storage-admission"), nil
}

// Watch runs independently of the sampler. A lost or stale monotonic pulse
// closes every local admission pipe before attempting SQL invalidation. That
// local path works even if SQL is unreachable or wall time is untrusted.
func Watch(ctx context.Context, p Publisher, in *os.File, guards []*os.File) error {
	if p == nil || in == nil || len(guards) == 0 {
		return ErrReport
	}
	deny := func() {
		for _, g := range guards {
			g.Close()
		}
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Unavailable(c)
	}
	defer deny()
	stop := context.AfterFunc(ctx, func() { in.Close() })
	defer stop()
	for {
		if in.SetReadDeadline(time.Now().Add(maxPulseAge)) != nil {
			return ErrReport
		}
		b := make([]byte, 8)
		if _, e := io.ReadFull(in, b); e != nil || !fresh(b) {
			return ErrReport
		}
		for _, g := range guards {
			if g.SetWriteDeadline(time.Now().Add(time.Second)) != nil {
				return ErrReport
			}
			out, e := pulse()
			if e != nil {
				return e
			}
			if _, e = g.Write(out); e != nil {
				return ErrReport
			}
		}
	}
}

type Guard struct {
	mu     sync.Mutex
	file   *os.File
	last   []byte
	closed bool
}

func NewGuard(f *os.File) *Guard { return &Guard{file: f} }
func (g *Guard) Alive() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.file == nil {
		return false
	}
	fd := int(g.file.Fd())
	buf := make([]byte, 4096)
	for {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP}}
		_, e := unix.Poll(fds, 0)
		if e != nil || fds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			g.closed = true
			return false
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			break
		}
		n, e := unix.Read(fd, buf)
		if e != nil || n == 0 || n%8 != 0 {
			g.closed = true
			return false
		}
		g.last = append(g.last[:0], buf[n-8:n]...)
	}
	return fresh(g.last)
}
func (g *Guard) Close() {
	if g != nil && g.file != nil {
		g.file.Close()
	}
}
