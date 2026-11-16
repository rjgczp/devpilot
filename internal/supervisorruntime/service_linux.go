//go:build privateworker && privateprobe && linux

package supervisorruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/retentioncontrol"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
	"github.com/rjgczp/devpilot/internal/workerruntime"
	"sync"
	"time"
)

type Database interface {
	scheduler.Database
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
type Backend interface {
	Call(context.Context, workerruntime.Call) (BackendReply, error)
}
type Supervisor struct {
	DB                             Database
	Incarnation, WorkerIncarnation string
	PublicKey                      []byte
	Kernel                         Backend
	Controller                     workerruntime.Supervisor
	active                         *handoff.Lineage
}

func newNamespace() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func (s *Supervisor) sql(ctx context.Context, q string, args ...any) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, e := s.DB.Exec(c, q, args...)
	return e
}
func (s *Supervisor) control(ctx context.Context, op string, l handoff.Lineage) error {
	r, e := s.Controller.Call(ctx, workerruntime.Call{Op: op, Line: &l})
	if e != nil || r.Event != op+"_ok" || r.Line != l {
		return ErrClosed
	}
	return nil
}
func (s *Supervisor) Lost() {
	if s.active != nil {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.control(c, "recover", *s.active)
	}
}
func (s *Supervisor) Handle(ctx context.Context, c workerruntime.Call) (workerruntime.Reply, error) {
	reply := workerruntime.Reply{Event: "closed"}
	if c.Op == "hello" && s.active == nil && c.Line == nil && c.Lease == nil && c.Hint == nil && len(c.Envelope) == 0 && len(c.Frame) == 0 {
		// Restart may reconcile immutable physical evidence only. It cannot
		// reconstruct an input envelope or send the old Run's GET again.
		old, e := s.Kernel.Call(ctx, workerruntime.Call{Op: "pending"})
		if e != nil {
			return reply, ErrClosed
		}
		if old.Event == "pending" {
			verified, e := supervisorfixture.VerifyCommittedStopBundle(old.Receipt, s.PublicKey, old.Bundle, old.Line)
			if e != nil || old.Line.SupervisorIncarnation != s.Incarnation {
				return reply, ErrClosed
			}
			settlement := supervisorfixture.Settlement{DB: s.DB, Incarnation: s.Incarnation}
			out, err := settlement.Reconcile(ctx, verified)
			if err != nil {
				return reply, ErrClosed
			}
			if out.State == "pending" {
				if _, err = settlement.Accept(ctx, verified); err != nil {
					return reply, ErrClosed
				}
			}
			if s.control(ctx, "recover", old.Line) != nil {
				return reply, ErrClosed
			}
			ack, e := s.Kernel.Call(ctx, workerruntime.Call{Op: "recovered", Line: &old.Line})
			if e != nil || ack.Event != "recovered" {
				return reply, ErrClosed
			}
		} else if old.Event != "none" {
			return reply, ErrClosed
		}
		reply.Event = "supervisor_ready"
		return reply, nil
	}
	if c.Op == "prepare" {
		if s.active != nil || c.Lease == nil || c.Hint == nil || c.Line != nil {
			return reply, ErrClosed
		}
		l, h := c.Lease, c.Hint
		if l.WorkspaceID != h.WorkspaceID || l.ProjectID != h.ProjectID || l.MonitorID != h.MonitorID || l.RunID != h.RunID || l.ConfigurationVersion != h.ConfigurationVersion || l.ActivationGeneration != h.ActivationGeneration || l.SchedulerEpoch != h.SchedulerEpoch || !l.LeaseUntil.After(time.Now()) {
			return reply, ErrClosed
		}
		ns, e := newNamespace()
		if e != nil {
			return reply, ErrClosed
		}
		line := handoff.Lineage{WorkspaceID: l.WorkspaceID, ProjectID: l.ProjectID, MonitorID: l.MonitorID, RunID: l.RunID, ConfigurationVersion: l.ConfigurationVersion, ActivationGeneration: l.ActivationGeneration, SchedulerEpoch: l.SchedulerEpoch, FencingToken: l.FencingToken, NamespaceIncarnation: ns, SupervisorIncarnation: s.Incarnation}
		if !line.Valid() {
			return reply, ErrClosed
		}
		s.active = &line
		ready, e := s.Kernel.Call(ctx, workerruntime.Call{Op: "prepare", Line: &line})
		if e != nil || ready.Event != "prepared" || len(ready.Ready) != 2 {
			return reply, ErrClosed
		}
		raw, e := json.Marshal(ready.Ready)
		if e != nil {
			return reply, ErrClosed
		}
		digest := sha256.Sum256(raw)
		if e = s.sql(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, s.Incarnation, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, s.WorkerIncarnation, ns, digest[:]); e != nil {
			return reply, ErrClosed
		}
		if s.control(ctx, "track", line) != nil {
			return reply, ErrClosed
		}
		return workerruntime.Reply{Event: "prepared", Line: line}, nil
	}
	if s.active == nil || c.Line == nil || *c.Line != *s.active {
		return reply, ErrClosed
	}
	line := *s.active
	reply.Line = line
	switch c.Op {
	case "consume":
		if s.sql(ctx, `SELECT scheduler_private.consume_input($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bigint)`, s.Incarnation, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, line.NamespaceIncarnation, line.FencingToken) != nil {
			return reply, ErrClosed
		}
		reply.Event = "input_committed"
	case "launch", "next", "reply", "install":
		if c.Op == "install" && (c.Request == nil || c.Permit == nil || c.Request.Lineage != line) {
			return reply, ErrClosed
		}
		r, e := s.Kernel.Call(ctx, c)
		if e != nil {
			return reply, ErrClosed
		}
		reply.Event = r.Event
		reply.Frame = r.Frame
		reply.Confirmation = r.Confirmation
		reply.Observation = r.Observation
	case "settle":
		if s.control(ctx, "quarantine", line) != nil {
			return reply, ErrClosed
		}
		r, e := s.Kernel.Call(ctx, workerruntime.Call{Op: "stop", Line: &line})
		if e != nil || r.Event != "stopped" {
			return reply, ErrClosed
		}
		verified, e := supervisorfixture.VerifyCommittedStopBundle(r.Receipt, s.PublicKey, r.Bundle, line)
		if e != nil {
			return reply, ErrClosed
		}
		if _, e = (supervisorfixture.Settlement{DB: s.DB, Incarnation: s.Incarnation}).Accept(ctx, verified); e != nil {
			return reply, ErrClosed
		}
		ack, e := s.Kernel.Call(ctx, workerruntime.Call{Op: "accepted", Line: &line})
		if e != nil || ack.Event != "accepted" {
			return reply, ErrClosed
		}
		if s.control(ctx, "untrack", line) != nil {
			return reply, ErrClosed
		}
		reply.Event = "settled"
		s.active = nil
	default:
		return reply, ErrClosed
	}
	return reply, nil
}

// Controller holds only its existing role. Tracking is bounded memory, not an
// execution capability. EOF recovery never signs proof or returns a slot.
type Controller struct {
	DB          Database
	Incarnation string
	mu          sync.Mutex
	tracked     map[string]handoff.Lineage
}

func (c *Controller) quarantine(ctx context.Context, l handoff.Lineage) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, e := c.DB.Exec(ctx, `SELECT scheduler_private.quarantine_physical($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint)`, c.Incarnation, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.FencingToken)
	return e
}
func (c *Controller) recover(ctx context.Context, l handoff.Lineage) error {
	// recover_started itself quarantines unreleased Runs; an already released
	// receipt must not be sent back through quarantine_physical.
	_, e := (scheduler.Store{DB: c.DB, Incarnation: c.Incarnation}).RecoverStarted(ctx, l, "worker_lost")
	return e
}
func (c *Controller) Handle(ctx context.Context, r workerruntime.Call) (workerruntime.Reply, error) {
	if r.Line == nil || !r.Line.Valid() || r.Lease != nil || r.Hint != nil || len(r.Frame) != 0 || len(r.Envelope) != 0 || len(r.Receipt) != 0 || r.Permit != nil || r.Request != nil {
		return workerruntime.Reply{}, ErrClosed
	}
	l := *r.Line
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tracked == nil {
		c.tracked = map[string]handoff.Lineage{}
	}
	switch r.Op {
	case "track":
		if len(c.tracked) >= 2 {
			return workerruntime.Reply{}, ErrClosed
		}
		c.tracked[l.RunID] = l
	case "untrack":
		if c.tracked[l.RunID] != l {
			return workerruntime.Reply{}, ErrClosed
		}
		delete(c.tracked, l.RunID)
	case "quarantine":
		if c.tracked[l.RunID] != l || c.quarantine(ctx, l) != nil {
			return workerruntime.Reply{}, ErrClosed
		}
	case "recover":
		if c.recover(ctx, l) != nil {
			return workerruntime.Reply{}, ErrClosed
		}
		delete(c.tracked, l.RunID)
	default:
		return workerruntime.Reply{}, ErrClosed
	}
	return workerruntime.Reply{Event: r.Op + "_ok", Line: l}, nil
}

// RecoveryHandle is the parent's completion channel, never an execution or
// general management capability. SQL remains authoritative for the Run state.
func (c *Controller) RecoveryHandle(ctx context.Context, r workerruntime.Call) (workerruntime.Reply, error) {
	if r.Op != "recover" {
		return workerruntime.Reply{}, ErrClosed
	}
	return c.Handle(ctx, r)
}
func (c *Controller) Lost() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.tracked {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = c.recover(ctx, l)
		cancel()
	}
}
func (c *Controller) Run(ctx context.Context, peers []*Peer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.Lost()

	type completion struct {
		maintenance bool
		err         error
	}
	done := make(chan completion, len(peers)+1)
	if len(peers) != 3 {
		return ErrClosed
	}
	for index, p := range peers {
		go func(index int, p *Peer) {
			local := &Controller{DB: c.DB, Incarnation: c.Incarnation}
			handler := local.Handle
			if index == 2 {
				handler = local.RecoveryHandle
			}
			err := Serve(ctx, p, handler)
			local.Lost()
			done <- completion{err: err}
		}(index, p)
	}
	go func() {
		done <- completion{maintenance: true, err: retentioncontrol.Run(ctx, scheduler.Store{DB: c.DB, Incarnation: c.Incarnation})}
	}()
	// EOF recovers only that peer. On shutdown, wait for all bounded recovery
	// transactions before closing the pool; the other peer can still settle.
	remaining := len(peers)
	var maintenanceErr error
	for remaining > 0 {
		r := <-done
		if r.maintenance {
			maintenanceErr = r.err
			cancel()
			continue
		}
		remaining--
	}
	if maintenanceErr != nil {
		return maintenanceErr
	}
	return ErrClosed
}
