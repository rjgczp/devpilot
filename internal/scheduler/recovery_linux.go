//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// ReconcileClaim only reads the original scoped identity. A confirmed lease is
// not an execution capability: Start must still validate current authority.
// Missing/expired/started runs never result in a Claim or Start retry here.
func (s Store) ReconcileClaim(ctx context.Context, uncertain ClaimUnknown) (Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	h := uncertain.Hint
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT scheduler_private.reconcile_claim($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid)`, s.Incarnation, h.WorkspaceID, h.ProjectID, h.MonitorID, h.RunID).Scan(&raw); err != nil {
		return Lease{}, databaseError(err)
	}
	var lease Lease
	if len(raw) == 0 || string(raw) == "null" {
		return Lease{}, ErrCandidate
	}
	if json.Unmarshal(raw, &lease) != nil || !lease.matches(h) {
		return Lease{}, ErrUnavailable
	}
	return lease, nil
}

type Expiry struct {
	RunID        string `json:"run_id"`
	AttemptState string `json:"attempt_state"`
	Released     bool   `json:"released"`
}

// ExpireUnstarted cannot renew a lease, replay a cycle or recover any input.
// A prepared namespace stays quarantined for independent termination proof.
func (s Store) ExpireUnstarted(ctx context.Context, lease Lease) (Expiry, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Expiry{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT scheduler_private.expire_unstarted($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint)`, s.Incarnation, lease.WorkspaceID, lease.ProjectID, lease.MonitorID, lease.RunID, lease.FencingToken).Scan(&raw); err != nil {
		return Expiry{}, databaseError(err)
	}
	var outcome Expiry
	if json.Unmarshal(raw, &outcome) != nil || outcome.RunID != lease.RunID || outcome.AttemptState != "expired" {
		return Expiry{}, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return Expiry{}, ErrCommitUnknown
	}
	return outcome, nil
}
