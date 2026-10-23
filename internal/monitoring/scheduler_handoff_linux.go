//go:build schedulerfixture && linux

package monitoring

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

// SchedulerInput keeps the original kernel private Snapshot. The fixture-only
// execution method uses that exact engine after the independent sandbox seals.
type SchedulerInput struct {
	snapshot       Snapshot
	lineage        handoff.Lineage
	deadline       time.Time
	permitAttempt  *atomic.Bool
	executeAttempt *atomic.Bool
}

func (SchedulerInput) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<sealed scheduler input>")
}
func (s SchedulerInput) Lineage() handoff.Lineage { return s.lineage }
func (s SchedulerInput) Valid() bool              { return s.snapshot.valid() && s.lineage.Valid() }
func ReceiveSchedulerInput(receiver *handoff.Receiver, input *os.File) (SchedulerInput, error) {
	e, err := receiver.Receive(input)
	if err != nil {
		return SchedulerInput{}, handoff.ErrInput
	}
	v, err := strconv.ParseInt(e.ConfigurationVersion, 10, 64)
	if err != nil {
		return SchedulerInput{}, handoff.ErrInput
	}
	s := Snapshot{scope: Scope{e.WorkspaceID, e.ProjectID, e.MonitorID}, version: v, name: "monitor", target: string(e.Target), method: "GET", interval: int64(e.IntervalSeconds), timeout: int64(e.TimeoutSeconds), expected: int64(e.ExpectedStatus)}
	if !s.valid() {
		return SchedulerInput{}, handoff.ErrInput
	}
	return SchedulerInput{snapshot: s, lineage: e.Lineage, deadline: e.PermitDeadline, permitAttempt: &atomic.Bool{}, executeAttempt: &atomic.Bool{}}, nil
}

// targetHook is private and used only by the independently protected runner.
// Copies of a received input share one attempt. Missing pipes/default nil deny.
func (s SchedulerInput) targetHook(client *targetpermit.Client) targetPermitHook {
	return func(ctx context.Context, c targetCandidate) error {
		if !s.Valid() || s.permitAttempt == nil || !s.permitAttempt.CompareAndSwap(false, true) || client == nil || c.scope != s.snapshot.scope || c.version != s.snapshot.version {
			return ErrTargetPermit
		}
		u, err := url.Parse(s.snapshot.target)
		if err != nil || c.host != u.Hostname() || c.scheme != u.Scheme {
			return ErrTargetPermit
		}
		deadline := c.deadline
		if s.deadline.Before(deadline) {
			deadline = s.deadline
		}
		ctx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		r := targetpermit.Request{Lineage: s.lineage, Host: c.host, Scheme: c.scheme, IP: c.endpoint.Addr(), Port: c.endpoint.Port(), Deadline: deadline}
		if client.Acquire(ctx, r) != nil {
			return ErrTargetPermit
		}
		return nil
	}
}
