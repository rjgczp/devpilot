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

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

var ErrResult = errors.New("invalid or consumed minimal result")

// MinimalResult owns only a copied, validated observation. It contains no URL,
// raw error or retry identifier. Copies share the same one-submission fuse.
// This is not a proof that a remote server produced an observation: the trusted
// fixture Worker reports the original kernel; PG independently checks lineage,
// Lease/Fence and the previously committed physical termination receipt.
type MinimalResult struct{ value *minimalResult }
type minimalResult struct {
	line     handoff.Lineage
	kind     string
	duration *int
	status   *int
	success  *bool
	failure  *string
	stop     *string
	used     atomic.Bool
}

func (MinimalResult) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<private minimal result>")
}

func NetworkResult(line handoff.Lineage, r *monitoring.Result) (MinimalResult, error) {
	if !line.Valid() || r == nil || r.WorkspaceID != line.WorkspaceID || r.ProjectID != line.ProjectID || r.MonitorID != line.MonitorID || r.ConfigurationVersion != line.ConfigurationVersion || r.StartedAt.IsZero() || r.FinishedAt.Before(r.StartedAt) || r.Duration < 0 || r.Duration > 30*time.Second {
		return MinimalResult{}, ErrResult
	}
	v := &minimalResult{line: line, kind: "network"}
	d := int(r.Duration / time.Millisecond)
	v.duration = &d
	success := r.Success
	v.success = &success
	if r.HTTPStatus != nil {
		n := *r.HTTPStatus
		if n < 200 || n > 599 {
			return MinimalResult{}, ErrResult
		}
		v.status = &n
	}
	if r.FailureCode != nil {
		c := string(*r.FailureCode)
		switch *r.FailureCode {
		case monitoring.DNSFailed, monitoring.BlockedTarget, monitoring.ConnectFailed, monitoring.TLSFailed, monitoring.Timeout, monitoring.Canceled, monitoring.ResponseLimit, monitoring.ProtocolError, monitoring.UnexpectedStatus:
		default:
			return MinimalResult{}, ErrResult
		}
		v.failure = &c
	}
	if success && (v.status == nil || v.failure != nil) || !success && (v.failure == nil || ((*v.failure == string(monitoring.UnexpectedStatus)) != (v.status != nil))) {
		return MinimalResult{}, ErrResult
	}
	return MinimalResult{v}, nil
}

// ControlResult cannot carry a network observation. Phase/reason combinations
// are fixed and independently validated in PG. An unprepared preflight may have
// no namespace identities; those are sent as SQL NULL, never invented proof.
func ControlResult(line handoff.Lineage, phase, reason string) (MinimalResult, error) {
	check := line
	if phase == "preflight" && line.NamespaceIncarnation == "" && line.SupervisorIncarnation == "" {
		check.NamespaceIncarnation = line.RunID
		check.SupervisorIncarnation = line.RunID
	}
	if !check.Valid() {
		return MinimalResult{}, ErrResult
	}
	allowed := false
	switch phase {
	case "preflight":
		switch reason {
		case "invalid_configuration", "resource_limited", "authorization_revoked", "authorization_expired", "emergency_stop", "retention_capacity":
			allowed = true
		}
	case "permit_denied":
		switch reason {
		case "egress_unavailable", "commit_unknown", "lease_expired", "authorization_revoked", "authorization_expired", "resource_limited", "emergency_stop":
			allowed = true
		}
	case "physical_stop_unconfirmed":
		switch reason {
		case "worker_lost", "egress_unavailable", "commit_unknown", "emergency_stop":
			allowed = true
		}
	}
	if !allowed {
		return MinimalResult{}, ErrResult
	}
	return MinimalResult{&minimalResult{line: line, kind: phase, stop: &reason}}, nil
}

// PersistMinimalResult attempts a single transaction. Even an unknown Commit
// burns the process-local submission. No retry, reconciliation, target call,
// projection, physical release or next scheduling cycle exists here.
func (s Store) PersistMinimalResult(ctx context.Context, outcome MinimalResult) error {
	v := outcome.value
	if v == nil || !v.used.CompareAndSwap(false, true) {
		return ErrResult
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	var ns, sup any
	if v.line.NamespaceIncarnation != "" {
		ns = v.line.NamespaceIncarnation
	}
	if v.line.SupervisorIncarnation != "" {
		sup = v.line.SupervisorIncarnation
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT scheduler_private.persist_minimal_result($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::text,$13::integer,$14::integer,$15::boolean,$16::text,$17::text)`, s.Incarnation, v.line.WorkspaceID, v.line.ProjectID, v.line.MonitorID, v.line.RunID, v.line.ConfigurationVersion, v.line.ActivationGeneration, v.line.SchedulerEpoch, v.line.FencingToken, ns, sup, v.kind, v.duration, v.status, v.success, v.failure, v.stop).Scan(&raw)
	if err != nil {
		return databaseError(err)
	}
	var reply struct {
		RunID string `json:"run_id"`
		State string `json:"state"`
	}
	want := "control_recorded"
	if v.kind == "network" {
		want = "completed"
	}
	if len(raw) > 256 || json.Unmarshal(raw, &reply) != nil || reply.RunID != v.line.RunID || reply.State != want {
		return ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return ErrCommitUnknown
	}
	return nil
}
