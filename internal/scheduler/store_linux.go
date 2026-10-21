//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const transactionBudget = 2 * time.Second

var (
	ErrUnavailable   = errors.New("scheduler unavailable")
	ErrCandidate     = errors.New("candidate unavailable")
	ErrCapacity      = errors.New("retention capacity")
	ErrCommitUnknown = errors.New("commit acknowledgement unknown")
)

type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Store struct {
	DB          Database
	Incarnation string
}

// ClaimUnknown retains the original cycle identity for read-only reconciliation.
// It never authorizes another Claim or an execution envelope.
type ClaimUnknown struct{ Hint Hint }

func (ClaimUnknown) Error() string { return "commit acknowledgement unknown" }
func (ClaimUnknown) Unwrap() error { return ErrCommitUnknown }

type Hint struct {
	WorkspaceID          string `json:"workspace_id"`
	ProjectID            string `json:"project_id"`
	MonitorID            string `json:"monitor_id"`
	OwnerUserID          string `json:"owner_user_id"`
	MembershipID         string `json:"membership_id"`
	ConfigurationVersion string `json:"configuration_version"`
	ActivationGeneration string `json:"activation_generation"`
	SchedulerEpoch       string `json:"scheduler_epoch"`
	RunID                string `json:"run_id"`
}
type Lease struct {
	WorkspaceID          string    `json:"workspace_id"`
	ProjectID            string    `json:"project_id"`
	MonitorID            string    `json:"monitor_id"`
	RunID                string    `json:"run_id"`
	ConfigurationVersion string    `json:"configuration_version"`
	ActivationGeneration string    `json:"activation_generation"`
	SchedulerEpoch       string    `json:"scheduler_epoch"`
	FencingToken         string    `json:"fencing_token"`
	ClaimedAt            time.Time `json:"claimed_at"`
	LeaseUntil           time.Time `json:"lease_until"`
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func databaseError(err error) error {
	var p *pgconn.PgError
	if errors.As(err, &p) {
		if (p.Code == "P0001" || p.Code == "23514") && p.Message == "retention_capacity" {
			return ErrCapacity
		}
		if p.Code == "P0001" {
			if p.Message == "retention_capacity" {
				return ErrCapacity
			}
			return ErrCandidate
		}
		if p.Code == "55P03" || p.Code == "40P01" || p.Code == "57014" {
			return ErrCandidate
		}
	}
	return ErrUnavailable
}
func (s Store) Candidates(ctx context.Context, after string) ([]Hint, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var cursor any
	if after != "" {
		cursor = after
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT scheduler_private.candidates($1::uuid,$2::uuid)`, s.Incarnation, cursor).Scan(&raw); err != nil {
		return nil, databaseError(err)
	}
	var hints []Hint
	if json.Unmarshal(raw, &hints) != nil || len(hints) > 64*8 {
		return nil, ErrUnavailable
	}
	return hints, nil
}
func (s Store) Claim(ctx context.Context, h Hint) (Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Lease{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT scheduler_private.claim($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bigint,$8::bigint,$9::bigint,$10::uuid)`, s.Incarnation, h.WorkspaceID, h.ProjectID, h.MonitorID, h.OwnerUserID, h.MembershipID, h.ConfigurationVersion, h.ActivationGeneration, h.SchedulerEpoch, h.RunID).Scan(&raw)
	if err != nil {
		return Lease{}, databaseError(err)
	}
	var lease Lease
	if json.Unmarshal(raw, &lease) != nil || !lease.matches(h) {
		return Lease{}, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return Lease{}, ClaimUnknown{Hint: h}
	}
	return lease, nil
}

// Tick bounds attempts and rotates by workspace. There is no per-monitor
// goroutine, unbounded retry or implicit retry of an unknown Claim commit.
// The caller must reconcile that exact run before it can attempt a Start.
func (s Store) Tick(ctx context.Context, after string) ([]Lease, string, error) {
	hints, err := s.Candidates(ctx, after)
	if err != nil {
		return nil, after, err
	}
	seen := make(map[string]bool)
	leases := make([]Lease, 0, 32)
	attempts := 0
	cursor := after
	for _, h := range hints {
		if seen[h.WorkspaceID] {
			continue
		}
		if attempts == 32 {
			break
		}
		attempts++
		cursor = h.WorkspaceID
		lease, e := s.Claim(ctx, h)
		if e == nil {
			seen[h.WorkspaceID] = true
			leases = append(leases, lease)
			continue
		}
		if errors.Is(e, ErrCommitUnknown) {
			seen[h.WorkspaceID] = true
			return leases, cursor, e
		}
		if !errors.Is(e, ErrCandidate) && !errors.Is(e, ErrCapacity) {
			return leases, cursor, e
		}
	}
	return leases, cursor, nil
}

func (l Lease) matches(h Hint) bool {
	return l.WorkspaceID == h.WorkspaceID && l.ProjectID == h.ProjectID && l.MonitorID == h.MonitorID && l.RunID == h.RunID && l.ConfigurationVersion == h.ConfigurationVersion && l.ActivationGeneration == h.ActivationGeneration && l.SchedulerEpoch == h.SchedulerEpoch && l.FencingToken != "" && !l.ClaimedAt.IsZero() && l.LeaseUntil.After(l.ClaimedAt)
}
