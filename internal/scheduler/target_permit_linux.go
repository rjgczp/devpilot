//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

// TargetSupervisor belongs to an independent trusted management transport.
// The fixture allocator implements this after independent namespace/firewall checks.
// A Worker self-report or a DB reservation is NOT a Supervisor confirmation.
type TargetSupervisor interface {
	InstallTarget(context.Context, targetpermit.Request, TargetPermit) (targetpermit.Confirmation, error)
}

// TargetController is bound to a process-local, confirmed Start capability.
// Copies share one attempt bit; even failure/unknown Commit burns this attempt.
type TargetController struct {
	start      *startInput
	store      Store
	hint       Hint
	directory  TargetDirectory
	supervisor TargetSupervisor
}

func (TargetController) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<sealed target controller>")
}

func (s StartSnapshot) TargetController(store Store, h Hint, d TargetDirectory, supervisor TargetSupervisor) *TargetController {
	return &TargetController{start: s.input, store: store, hint: h, directory: d, supervisor: supervisor}
}
func (c *TargetController) Authorize(ctx context.Context, r targetpermit.Request) (targetpermit.Confirmation, error) {
	deny := func() (targetpermit.Confirmation, error) { return targetpermit.Confirmation{}, targetpermit.ErrDenied }
	if c == nil || c.start == nil || !c.start.targetAttempt.CompareAndSwap(false, true) || c.supervisor == nil || c.store.DB == nil || !r.Valid() {
		return deny()
	}
	v := c.start.wire
	expected := handoff.Lineage{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID, ConfigurationVersion: v.ConfigurationVersion, ActivationGeneration: v.ActivationGeneration, SchedulerEpoch: v.SchedulerEpoch, FencingToken: v.FencingToken, NamespaceIncarnation: v.NamespaceIncarnation, SupervisorIncarnation: v.SupervisorIncarnation}
	if r.Lineage != expected || !v.Lease.matches(c.hint) || r.Deadline.After(v.PermitDeadline) || !r.Deadline.After(time.Now()) {
		return deny()
	}
	u, e := url.Parse(v.URL)
	if e != nil || u.Hostname() != r.Host || u.Scheme != r.Scheme || u.Host != net.JoinHostPort(r.Host, "443") && u.Host != net.JoinHostPort(r.Host, "80") && u.Host != r.Host {
		return deny()
	}
	entry, e := c.directory.Bind(c.hint, v.URL, r.IP)
	if e != nil || len(entry.Keys) == 0 {
		return deny()
	}
	ctx, cancel := context.WithDeadline(ctx, r.Deadline)
	defer cancel()
	// Reuse the existing restricted VOLATILE function: User -> W -> parents ->
	// Run -> ordered target Budgets -> Reservation; final fresh PG gates unchanged.
	// Only the newest registered HMAC generation is accepted; never fall back.
	p, e := c.store.ReserveTarget(ctx, c.hint, v.Lease, entry.Keys[len(entry.Keys)-1])
	if e != nil || ctx.Err() != nil || p.PermitDeadline.Before(r.Deadline) {
		return deny()
	}
	// Commit has returned the connection before any external installation call.
	a, e := c.supervisor.InstallTarget(ctx, r, p)
	if e != nil || ctx.Err() != nil || a.PermitID != p.PermitID || !a.Matches(r) {
		return deny()
	}
	return a, nil
}
