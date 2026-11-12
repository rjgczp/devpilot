//go:build (schedulerfixture || privateworker) && linux

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

var ErrClosed = errors.New("worker admission closed")

// Worker owns one in-process Claim/Start attempt. SQL remains authoritative for
// Owner, fence, physical release and Result identity. It has no Probe launcher,
// network-management transport or ability to change the deployment gate.
type Worker struct {
	store          Store
	mu             sync.Mutex
	admission      bool
	hint           Hint
	lease          *Lease
	startAttempted bool
	uncertain      bool
}

func (*Worker) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<scheduler worker>") }

// NewClosedWorker is the only constructor in a privateworker-only build.
// No environment variable, database fixture gate or reconnect can open it.
func NewClosedWorker(store Store) *Worker { return &Worker{store: store} }

func (w *Worker) Candidates(ctx context.Context, after string) ([]Hint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.admission {
		return nil, ErrClosed
	}
	if w.lease != nil || w.uncertain {
		return nil, ErrCandidate
	}
	return w.store.Candidates(ctx, after)
}

func (w *Worker) Claim(ctx context.Context, h Hint) (Lease, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.admission {
		return Lease{}, ErrClosed
	}
	if w.lease != nil || w.uncertain {
		return Lease{}, ErrCandidate
	}
	l, err := w.store.Claim(ctx, h)
	if errors.Is(err, ErrCommitUnknown) {
		w.uncertain = true
	}
	if err == nil {
		w.hint = h
		w.lease = &l
		w.startAttempted = false
	}
	return l, err
}

func (w *Worker) Start(ctx context.Context, h Hint, l Lease) (StartSnapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.admission {
		return StartSnapshot{}, ErrClosed
	}
	if w.lease == nil || h != w.hint || l != *w.lease || w.startAttempted {
		return StartSnapshot{}, ErrInputConsumed
	}
	// Burn before SQL. In particular an unknown Start commit is never replayed.
	w.startAttempted = true
	return w.store.Start(ctx, h, l)
}

// Closing new admission must not disable settlement. These calls reuse the
// original fixed-ID Result protocol and cannot manufacture a physical release.
func (w *Worker) CommitResult(ctx context.Context, c ResultCommit) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.store.CommitResult(ctx, c)
	if err == nil {
		w.complete(c)
	}
	return err
}
func (w *Worker) ReconcileResult(ctx context.Context, c ResultCommit) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ok, err := w.store.ReconcileResult(ctx, c)
	if err == nil && ok {
		w.complete(c)
	}
	return ok, err
}
func (w *Worker) complete(c ResultCommit) {
	if w.lease != nil && c.value != nil && c.value.line.RunID == w.lease.RunID &&
		c.value.line.WorkspaceID == w.lease.WorkspaceID && c.value.line.ProjectID == w.lease.ProjectID &&
		c.value.line.MonitorID == w.lease.MonitorID && c.value.line.ConfigurationVersion == w.lease.ConfigurationVersion &&
		c.value.line.ActivationGeneration == w.lease.ActivationGeneration && c.value.line.SchedulerEpoch == w.lease.SchedulerEpoch &&
		c.value.line.FencingToken == w.lease.FencingToken {
		w.lease = nil
		w.startAttempted = false
	}
}
