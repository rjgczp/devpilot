//go:build privateprobe && linux

package monitoring

import (
	"context"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"golang.org/x/sys/unix"
)

// SupervisorAlive accepts only an inherited read-only anonymous pipe. The
// independent PID1 Supervisor retains its sole writer; Probe cannot renew it.
func SupervisorAlive(f *os.File) bool {
	if f == nil {
		return false
	}
	fd := int(f.Fd())
	var st unix.Stat_t
	flags, e := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	if e != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFIFO {
		return false
	}
	link, e := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	if e != nil || !strings.HasPrefix(link, "pipe:[") {
		return false
	}
	p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	_, e = unix.Poll(p, 0)
	return e == nil && p[0].Revents == 0
}

// ExecutePrivate preserves the one-use engine and its DNS/literal/TLS policy.
// No caller-supplied resolver, roots, client, retry or alternate address exists.
func (s SchedulerInput) ExecutePrivate(ctx context.Context, client *targetpermit.Client, supervisor *os.File) (*Result, error) {
	if !s.Valid() || s.executeAttempt == nil || !s.executeAttempt.CompareAndSwap(false, true) || client == nil || !SupervisorAlive(supervisor) {
		return nil, ErrTargetPermit
	}
	u, e := url.Parse(s.snapshot.target)
	if e != nil || u.Scheme != "https" {
		return nil, ErrTargetPermit
	}
	ctx, cancel := context.WithDeadline(ctx, s.deadline)
	defer cancel()
	engine, e := newEngine(netip.MustParseAddr("192.0.2.53"), nil)
	if e != nil {
		return nil, e
	}
	acquire := s.targetHook(client)
	engine.targetPermit = func(c context.Context, r targetCandidate) error {
		if !SupervisorAlive(supervisor) || r.scheme != "https" || r.endpoint.Port() != 443 {
			return ErrTargetPermit
		}
		if e := acquire(c, r); e != nil {
			return e
		}
		if !SupervisorAlive(supervisor) {
			return ErrTargetPermit
		}
		return nil
	}
	return engine.execute(ctx, s.snapshot)
}
