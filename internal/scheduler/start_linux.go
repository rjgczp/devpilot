//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

var ErrInputConsumed = errors.New("execution input already consumed")

// StartSnapshot is process-local and redacts all generic representations. It
// cannot be reconstructed from a run. Its only serialization is a bounded,
// once-only execution envelope written after a confirmed Start commit.
type StartSnapshot struct{ input *startInput }
type startInput struct {
	wire          startWire
	consumed      atomic.Bool
	targetAttempt atomic.Bool
}
type startWire struct {
	Lease
	StartedAt             time.Time `json:"started_at"`
	PermitDeadline        time.Time `json:"permit_deadline"`
	URL                   string    `json:"url"`
	TimeoutSeconds        int       `json:"timeout_seconds"`
	ExpectedStatus        int       `json:"expected_status"`
	IntervalSeconds       int       `json:"interval_seconds"`
	NamespaceIncarnation  string    `json:"namespace_incarnation"`
	SupervisorIncarnation string    `json:"supervisor_incarnation"`
}

func (StartSnapshot) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<scheduler start snapshot>")
}

// takeFrame consumes the process-local sending capability before any IPC or DB
// input-consumption call. Every failure burns it; no reconnect restores it.
func (s StartSnapshot) takeFrame() ([]byte, handoff.Receipt, error) {
	if s.input == nil || !s.input.consumed.CompareAndSwap(false, true) {
		return nil, handoff.Receipt{}, ErrInputConsumed
	}
	v := s.input.wire
	e := handoff.Envelope{Lineage: handoff.Lineage{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID, ConfigurationVersion: v.ConfigurationVersion, ActivationGeneration: v.ActivationGeneration, SchedulerEpoch: v.SchedulerEpoch, FencingToken: v.FencingToken, NamespaceIncarnation: v.NamespaceIncarnation, SupervisorIncarnation: v.SupervisorIncarnation}, StartedAt: v.StartedAt, PermitDeadline: v.PermitDeadline, Target: []byte(v.URL), TimeoutSeconds: v.TimeoutSeconds, ExpectedStatus: v.ExpectedStatus, IntervalSeconds: v.IntervalSeconds}
	frame, receipt, err := handoff.Encode(e)
	if err != nil {
		return nil, handoff.Receipt{}, ErrUnavailable
	}
	return frame, receipt, nil
}

// writeInput is a private codec test surface, not a launch or dispatch API.
func (s StartSnapshot) writeInput(w io.Writer) error {
	frame, _, err := s.takeFrame()
	if err != nil {
		return err
	}
	n, err := w.Write(frame)
	if err != nil || n != len(frame) {
		return ErrUnavailable
	}
	return nil
}

func (s Store) Start(ctx context.Context, h Hint, lease Lease) (StartSnapshot, error) {
	if lease.RunID != h.RunID || lease.WorkspaceID != h.WorkspaceID || lease.ProjectID != h.ProjectID || lease.MonitorID != h.MonitorID {
		return StartSnapshot{}, ErrCandidate
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return StartSnapshot{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT scheduler_private.start_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,$8::bigint)`, s.Incarnation, h.WorkspaceID, h.ProjectID, h.MonitorID, h.OwnerUserID, h.MembershipID, h.RunID, lease.FencingToken).Scan(&raw)
	if err != nil {
		return StartSnapshot{}, databaseError(err)
	}
	var v startWire
	if json.Unmarshal(raw, &v) != nil || v.RunID != h.RunID || v.WorkspaceID != h.WorkspaceID || v.ProjectID != h.ProjectID || v.MonitorID != h.MonitorID || v.ConfigurationVersion != lease.ConfigurationVersion || v.ActivationGeneration != lease.ActivationGeneration || v.SchedulerEpoch != lease.SchedulerEpoch || v.FencingToken != lease.FencingToken || v.NamespaceIncarnation == "" || v.SupervisorIncarnation == "" || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 30 || v.ExpectedStatus < 200 || v.ExpectedStatus > 599 || v.IntervalSeconds < 60 || len(v.URL) == 0 || len(v.URL) > 2048 || !utf8.ValidString(v.URL) || v.StartedAt.IsZero() || v.PermitDeadline.Before(v.StartedAt.Add(time.Duration(v.TimeoutSeconds)*time.Second)) || v.PermitDeadline.After(v.StartedAt.Add(35*time.Second)) {
		return StartSnapshot{}, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return StartSnapshot{}, ErrCommitUnknown
	}
	// Commit has released the pool connection before offline validation/IPC.
	if !monitor.ValidStoredConfiguration("monitor", v.URL, "GET", int64(v.IntervalSeconds), int64(v.TimeoutSeconds), int64(v.ExpectedStatus)) {
		return StartSnapshot{}, ErrUnavailable
	}
	// start_run returns lineage but intentionally omits Claim timestamps. Retain
	// the already-validated original Lease for the later target reservation; no
	// readback, reconstruction or privilege is introduced. PG rechecks it there.
	v.Lease = lease
	return StartSnapshot{input: &startInput{wire: v}}, nil
}
