package monitoring

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"
)

// ErrTargetPermit is a control-plane refusal, never a target HTTP observation.
var ErrTargetPermit = errors.New("target_permit_unavailable")

// No public constructor or release-build caller can set this hook. Nil denies.
// The hook sees only the already validated origin/literal, never the URL path.
type targetCandidate struct {
	scope        Scope
	version      int64
	scheme, host string
	endpoint     netip.AddrPort
	deadline     time.Time
}

func (targetCandidate) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<private target candidate>")
}

type targetPermitHook func(context.Context, targetCandidate) error

// Called at the sole original TCP dial site. The original probe deadline bounds
// the hook and the original TCP/TLS stages still have their own smaller budgets.
// No asynchronous permission call, retry, fallback address or cached allow exists.
func permittedDial(ctx context.Context, c targetCandidate, hook targetPermitHook, dial func() (net.Conn, error)) (net.Conn, error) {
	if hook == nil || ctx.Err() != nil || !c.deadline.After(time.Now()) {
		return nil, ErrTargetPermit
	}
	if err := hook(ctx, c); err != nil || ctx.Err() != nil || !c.deadline.After(time.Now()) {
		return nil, ErrTargetPermit
	}
	return dial()
}
