//go:build privateworker && linux

package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

// Supervisor is a private, bounded management transport. A reply is never a
// physical-release proof; the original PG commit_result checks that separately.
// No DB connection spans a call; Supervisor credentials never enter the Worker.
type Supervisor interface {
	Call(context.Context, Call) (Reply, error)
}
type Call struct {
	Op       string                  `json:"op"`
	Hint     *scheduler.Hint         `json:"hint,omitempty"`
	Lease    *scheduler.Lease        `json:"lease,omitempty"`
	Line     *handoff.Lineage        `json:"line,omitempty"`
	Envelope []byte                  `json:"envelope,omitempty"`
	Receipt  []byte                  `json:"receipt,omitempty"`
	Frame    []byte                  `json:"frame,omitempty"`
	Permit   *scheduler.TargetPermit `json:"permit,omitempty"`
	Request  *targetpermit.Request   `json:"request,omitempty"`
}
type Reply struct {
	Event        string                     `json:"event"`
	Line         handoff.Lineage            `json:"line"`
	Frame        []byte                     `json:"frame,omitempty"`
	Confirmation *targetpermit.Confirmation `json:"confirmation,omitempty"`
	Observation  *monitoring.Result         `json:"observation,omitempty"`
}

func (Call) Format(f fmt.State, _ rune)  { _, _ = io.WriteString(f, "<private Supervisor request>") }
func (Reply) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<private Supervisor reply>") }

type PrivateLoop struct {
	store        scheduler.Store
	worker       *scheduler.Worker
	storageGuard func() bool
}

// RequireStorageGuard adds a deny-only inherited monotonic watchdog. It grants
// no SQL authority and is never consulted by Result/physical settlement.
func (l *PrivateLoop) RequireStorageGuard(alive func() bool) { l.storageGuard = alive }
func (l *PrivateLoop) storageAlive() bool                    { return l.storageGuard == nil || l.storageGuard() }

func OpenPrivate(ctx context.Context, cfg *pgxpool.Config, incarnation string) (*PrivateLoop, func(), error) {
	pool, e := openPool(ctx, cfg, incarnation)
	if e != nil {
		return nil, func() {}, e
	}
	store := scheduler.Store{DB: pool, Incarnation: incarnation}
	w := scheduler.NewPrivateWorker(store)
	if !w.PrivateReady(ctx) {
		pool.Close()
		return nil, func() {}, scheduler.ErrClosed
	}
	return &PrivateLoop{store: store, worker: w}, pool.Close, nil
}

// Run never reconstructs a started Run. Unknown Claim/Start/input/Result or lost
// Supervisor stops this process; a restart cannot replay an old sending input.
func (l *PrivateLoop) Run(ctx context.Context, sup Supervisor, directory scheduler.TargetDirectory, event func(string, string)) error {
	if l == nil || sup == nil {
		return ErrBoundary
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !l.storageAlive() {
			return fmt.Errorf("storage watchdog before candidates: %w", scheduler.ErrClosed)
		}
		hints, e := l.worker.Candidates(ctx, "")
		if e != nil && !errors.Is(e, scheduler.ErrClosed) {
			return e
		}
		for _, h := range hints {
			if !l.storageAlive() {
				return fmt.Errorf("storage watchdog before claim: %w", scheduler.ErrClosed)
			}
			lease, e := l.worker.Claim(ctx, h)
			if errors.Is(e, scheduler.ErrCandidate) || errors.Is(e, scheduler.ErrCapacity) {
				continue
			}
			if e != nil {
				return e
			}
			if event != nil {
				event("claimed", lease.RunID)
			}
			if e = l.execute(ctx, sup, directory, h, lease); e != nil {
				return e
			}
			if event != nil {
				event("result_committed", lease.RunID)
			}
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (l *PrivateLoop) execute(ctx context.Context, sup Supervisor, directory scheduler.TargetDirectory, h scheduler.Hint, lease scheduler.Lease) error {
	ctx, cancel := context.WithDeadline(ctx, lease.LeaseUntil)
	defer cancel()
	prepared, e := sup.Call(ctx, Call{Op: "prepare", Hint: &h, Lease: &lease})
	if e != nil {
		return e
	}
	line := prepared.Line
	if prepared.Event != "prepared" || !matches(line, lease) {
		return ErrBoundary
	}
	if !l.storageAlive() {
		return fmt.Errorf("storage watchdog before start: %w", scheduler.ErrClosed)
	}
	snapshot, e := l.worker.Start(ctx, h, lease)
	if e != nil {
		return e
	}
	pipes, e := snapshot.IssueInput(ctx, scheduler.InputController{DB: inputDB{sup: sup, line: line}, Incarnation: line.SupervisorIncarnation})
	if e != nil {
		return e
	}
	defer pipes.Close()
	envelope, e := io.ReadAll(io.LimitReader(pipes.Envelope, handoff.FrameSize+1))
	if e != nil || len(envelope) != handoff.FrameSize {
		return ErrBoundary
	}
	receipt, e := io.ReadAll(io.LimitReader(pipes.Receipt, handoff.ReceiptSize+1))
	if e != nil || len(receipt) != handoff.ReceiptSize {
		return ErrBoundary
	}
	pipes.Close()
	ready, e := sup.Call(ctx, Call{Op: "launch", Line: &line, Envelope: envelope, Receipt: receipt})
	if e != nil || ready.Event != "sealed" {
		return ErrBoundary
	}
	next, e := sup.Call(ctx, Call{Op: "next", Line: &line})
	if e != nil {
		return e
	}
	if next.Event == "target_request" {
		controller := snapshot.TargetController(l.store, h, directory, targetSupervisor{sup: sup, line: line, storageAlive: l.storageAlive})
		reply, e := targetReply(ctx, next.Frame, controller.Authorize)
		// A refusal is a zero-byte EOF, not an invented success/HTTP observation.
		if e != nil {
			reply = nil
		}
		if _, e = sup.Call(ctx, Call{Op: "reply", Line: &line, Frame: reply}); e != nil {
			return e
		}
		next, e = sup.Call(ctx, Call{Op: "next", Line: &line})
		if e != nil {
			return e
		}
	}
	if next.Event != "observation" {
		return ErrBoundary
	}
	// Root Supervisor must quarantine, perform original physical stop and commit
	// the original verified receipt. No boolean flag can authorize slot return.
	settled, err := sup.Call(ctx, Call{Op: "settle", Line: &line})
	if err != nil {
		return err
	}
	if settled.Event != "settled" {
		return ErrBoundary
	}
	if next.Observation == nil {
		outcome, e := scheduler.NewControlOutcome(line, "permit_denied", "egress_unavailable")
		if e != nil {
			return e
		}
		if e = l.store.PersistControlResult(ctx, outcome); e != nil {
			return e
		}
		return scheduler.ErrUnavailable
	}
	outcome, e := scheduler.NetworkResult(line, next.Observation)
	if e != nil {
		return e
	}
	commit, e := scheduler.NewResultCommit(outcome)
	if e != nil {
		return e
	}
	// No automatic retry even of a lost Result ACK. Never sends another GET.
	if e = l.worker.CommitResult(ctx, commit); e != nil {
		return e
	}
	seen, e := l.worker.ReconcileResult(ctx, commit)
	if e != nil || !seen {
		return scheduler.ErrCommitUnknown
	}
	return nil
}
func matches(line handoff.Lineage, l scheduler.Lease) bool {
	return line.Valid() && line.WorkspaceID == l.WorkspaceID && line.ProjectID == l.ProjectID && line.MonitorID == l.MonitorID && line.RunID == l.RunID && line.ConfigurationVersion == l.ConfigurationVersion && line.ActivationGeneration == l.ActivationGeneration && line.SchedulerEpoch == l.SchedulerEpoch && line.FencingToken == l.FencingToken
}

type targetSupervisor struct {
	sup          Supervisor
	line         handoff.Lineage
	storageAlive func() bool
}

func (s targetSupervisor) InstallTarget(ctx context.Context, r targetpermit.Request, p scheduler.TargetPermit) (targetpermit.Confirmation, error) {
	if s.storageAlive != nil && !s.storageAlive() {
		return targetpermit.Confirmation{}, targetpermit.ErrDenied
	}
	if r.Lineage != s.line {
		return targetpermit.Confirmation{}, ErrBoundary
	}
	reply, e := s.sup.Call(ctx, Call{Op: "install", Line: &s.line, Request: &r, Permit: &p})
	if e != nil || reply.Confirmation == nil || !reply.Confirmation.Matches(r) {
		return targetpermit.Confirmation{}, targetpermit.ErrDenied
	}
	return *reply.Confirmation, nil
}
func targetReply(ctx context.Context, frame []byte, handle func(context.Context, targetpermit.Request) (targetpermit.Confirmation, error)) ([]byte, error) {
	if len(frame) != targetpermit.FrameSize {
		return nil, targetpermit.ErrDenied
	}
	rr, rw, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	defer rr.Close()
	defer rw.Close()
	ar, aw, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	defer ar.Close()
	defer aw.Close()
	if _, e = rw.Write(frame); e != nil {
		return nil, e
	}
	rw.Close()
	e = (&targetpermit.Server{}).Serve(ctx, rr, aw, handle)
	if e != nil {
		return nil, e
	}
	return io.ReadAll(io.LimitReader(ar, targetpermit.FrameSize+1))
}
