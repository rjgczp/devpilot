//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

// Recovery is a terminal control fact, never an HTTP result or input capability.
// NextRunID names a different, future cycle; Claim and Start reauthorize it.
type Recovery struct {
	RunID        string     `json:"run_id"`
	AttemptState string     `json:"attempt_state"`
	Released     bool       `json:"released"`
	NextRunID    *string    `json:"next_run_id"`
	NextDueAt    *time.Time `json:"next_due_at"`
}

// RecoverStarted uses a Controller SQL identity and a known complete lineage.
// It never discovers arbitrary Runs, recreates input, calls the network, accepts
// physical proof or returns slots. Commit uncertainty preserves the identity;
// repeat only this control operation, never the original Probe/GET.
func (s Store) RecoverStarted(ctx context.Context, l handoff.Lineage, reason string) (Recovery, error) {
	if !l.Valid() {
		return Recovery{}, ErrCandidate
	}
	switch reason {
	case "lease_expired", "worker_lost", "commit_unknown", "emergency_stop", "egress_unavailable":
	default:
		return Recovery{}, ErrCandidate
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return Recovery{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT scheduler_private.recover_started($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::text)`, s.Incarnation, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.ConfigurationVersion, l.ActivationGeneration, l.SchedulerEpoch, l.FencingToken, l.NamespaceIncarnation, l.SupervisorIncarnation, reason).Scan(&raw)
	if e != nil {
		return Recovery{}, databaseError(e)
	}
	var r Recovery
	if len(raw) > 1024 || json.Unmarshal(raw, &r) != nil || r.RunID != l.RunID || (r.AttemptState != "unknown" && r.AttemptState != "canceled" && r.AttemptState != "completed") || (r.NextRunID == nil) != (r.NextDueAt == nil) || (r.NextRunID != nil && (!r.Released || *r.NextRunID == l.RunID)) {
		return Recovery{}, ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return Recovery{}, ErrCommitUnknown
	}
	return r, nil
}
