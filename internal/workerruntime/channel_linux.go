//go:build privateworker && linux

package workerruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"golang.org/x/sys/unix"
)

const supervisorFrameLimit = 64 << 10

// PipeSupervisor is only inherited anonymous IPC to the independently trusted
// parent. There is no reconnect, listener, generic SQL or management socket.
type PipeSupervisor struct {
	mu      sync.Mutex
	in, out *os.File
	closed  bool
	reader  *bufio.Reader
}

func InheritedSupervisor() (*PipeSupervisor, error) {
	if unix.SetNonblock(3, true) != nil || unix.SetNonblock(4, true) != nil {
		return nil, ErrBoundary
	}
	return newPipeSupervisor(os.NewFile(3, "supervisor-replies"), os.NewFile(4, "supervisor-requests"))
}
func newPipeSupervisor(in, out *os.File) (*PipeSupervisor, error) {
	for i, f := range []*os.File{in, out} {
		if f == nil {
			return nil, ErrBoundary
		}
		fd := int(f.Fd())
		var stat unix.Stat_t
		flags, e := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
		link, le := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
		expected := unix.O_RDONLY
		if i == 1 {
			expected = unix.O_WRONLY
		}
		if e != nil || le != nil || flags&unix.O_ACCMODE != expected || unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFIFO || !strings.HasPrefix(link, "pipe:[") || unix.SetNonblock(fd, true) != nil {
			return nil, ErrBoundary
		}
	}
	return &PipeSupervisor{in: in, out: out, reader: bufio.NewReaderSize(in, supervisorFrameLimit+1)}, nil
}
func (p *PipeSupervisor) Close() {
	if p != nil {
		p.in.Close()
		p.out.Close()
	}
}
func (p *PipeSupervisor) Call(ctx context.Context, c Call) (Reply, error) {
	if p == nil {
		return Reply{}, ErrBoundary
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || ctx.Err() != nil {
		return Reply{}, ErrBoundary
	}
	raw, e := json.Marshal(c)
	if e != nil || len(raw) > supervisorFrameLimit-1 {
		return Reply{}, ErrBoundary
	}
	// OS lifecycle waits have no open SQL connection and remain below the
	// immutable Lease/Permit deadline. SQL/input commits retain their own2s cap.
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if p.in.SetReadDeadline(deadline) != nil || p.out.SetWriteDeadline(deadline) != nil {
		return Reply{}, ErrBoundary
	}
	stop := context.AfterFunc(ctx, p.Close)
	defer stop()
	fail := func() (Reply, error) { p.closed = true; p.Close(); return Reply{}, ErrBoundary }
	if _, e = p.out.Write(append(raw, '\n')); e != nil {
		return fail()
	}
	response, e := p.reader.ReadSlice('\n')
	if e != nil || len(response) > supervisorFrameLimit || ctx.Err() != nil || !credentials.ValidJSON(response) {
		return fail()
	}
	var r Reply
	d := json.NewDecoder(bytes.NewReader(response))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil {
		return fail()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fail()
	}
	if c.Line != nil && r.Line != *c.Line {
		return fail()
	}
	return r, nil
}

// TargetCatalogFile is an operator secret, not arbitrary user input. Its only
// effect is to restrict the existing target directory; it cannot open a Gate.
func TargetCatalogFile(path string) (scheduler.TargetDirectory, error) {
	f, e := os.Open(path)
	if e != nil {
		return scheduler.TargetDirectory{}, ErrConfiguration
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil || len(raw) > 8192 || !credentials.ValidJSON(raw) {
		return scheduler.TargetDirectory{}, ErrConfiguration
	}
	var c struct {
		Generation int64                     `json:"generation"`
		Key        []byte                    `json:"key"`
		Targets    []scheduler.FixtureTarget `json:"targets"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return scheduler.TargetDirectory{}, ErrConfiguration
	}
	dir, e := scheduler.NewTargetDirectory(map[int64][]byte{c.Generation: c.Key}, c.Targets)
	if e != nil {
		return scheduler.TargetDirectory{}, ErrConfiguration
	}
	return dir, nil
}
