//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
)

// ResultCommit is an immutable report and stable identity, never an execution
// capability. Copies share only immutable fields; explicit SQL retries are safe.
type ResultCommit struct {
	value *minimalResult
	id    string
}

func (ResultCommit) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<private result commit>") }
func NewResultCommit(outcome MinimalResult) (ResultCommit, error) {
	v := outcome.value
	if v == nil || v.kind != "network" || v.used.Load() {
		return ResultCommit{}, ErrResult
	}
	var id [16]byte
	if _, e := rand.Read(id[:]); e != nil {
		return ResultCommit{}, ErrUnavailable
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	return ResultCommit{value: v, id: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])}, nil
}
func (c ResultCommit) args(inc string) []any {
	l := c.value.line
	return []any{inc, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.ConfigurationVersion, l.ActivationGeneration, l.SchedulerEpoch, l.FencingToken, l.NamespaceIncarnation, l.SupervisorIncarnation, c.id}
}

// CommitResult retries only a previously frozen report. It cannot launch a
// Probe, return a physical slot or interpret an unknown Commit as rollback.
func (s Store) CommitResult(ctx context.Context, c ResultCommit) error {
	if c.value == nil || c.id == "" {
		return ErrResult
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	a := append(c.args(s.Incarnation), c.value.duration, c.value.status, c.value.success, c.value.failure)
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT scheduler_private.commit_result($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::uuid,$13::integer,$14::integer,$15::boolean,$16::text)`, a...).Scan(&raw)
	if e != nil {
		return databaseError(e)
	}
	var reply resultCommitReply
	if len(raw) > 1024 || json.Unmarshal(raw, &reply) != nil || reply.RunID != c.value.line.RunID || reply.CommitID != c.id || reply.State != "committed" {
		return ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return ErrCommitUnknown
	}
	return nil
}

type resultCommitReply struct {
	RunID    string  `json:"run_id"`
	CommitID string  `json:"commit_id"`
	State    string  `json:"state"`
	Duration *int    `json:"duration_ms"`
	Status   *int    `json:"http_status"`
	Success  *bool   `json:"success"`
	Failure  *string `json:"failure_code"`
}

func equalOptional[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// ReconcileResult returns false,nil only for a valid scoped snapshot in which
// the commit was not observed. It is not proof of rollback or execution absence.
// Any database/identity uncertainty is an error, never false,nil.
func (s Store) ReconcileResult(ctx context.Context, c ResultCommit) (bool, error) {
	if c.value == nil || c.id == "" {
		return false, ErrResult
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return false, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT scheduler_private.reconcile_result($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::uuid)`, c.args(s.Incarnation)...).Scan(&raw)
	if e != nil {
		return false, databaseError(e)
	}
	var r resultCommitReply
	if len(raw) > 1024 || json.Unmarshal(raw, &r) != nil || r.RunID != c.value.line.RunID || r.CommitID != c.id {
		return false, ErrUnavailable
	}
	if r.State == "not_observed" {
		return false, nil
	}
	v := c.value
	if r.State != "committed" || !equalOptional(r.Duration, v.duration) || !equalOptional(r.Status, v.status) || !equalOptional(r.Success, v.success) || !equalOptional(r.Failure, v.failure) {
		return false, ErrResult
	}
	return true, nil
}
