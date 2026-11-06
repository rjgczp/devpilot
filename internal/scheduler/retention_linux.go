//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// CleanupHint is not authority. SQL rechecks the complete scope after all locks.
// Target keys are control-plane digests; never return them through a user API.
type CleanupHint struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	MonitorID   string `json:"monitor_id,omitempty"`
	Kind        string `json:"kind,omitempty"`
	BudgetKey   string `json:"budget_key,omitempty"`
}
type CleanupPage struct {
	Items      []CleanupHint `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}
type CleanupCursor struct {
	Finished *time.Time `json:"next_finished"`
	RunID    *string    `json:"next_run"`
}
type CleanupOutcome struct {
	CleanupCursor
	DeletedRuns int    `json:"deleted_runs"`
	DeletedKeys int    `json:"deleted_keys"`
	RowsWritten int    `json:"rows_written"`
	Reason      string `json:"reason"`
}

func (s Store) RetentionCandidates(ctx context.Context, cursor *string, limit int) (CleanupPage, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT scheduler_private.retention_candidates($1::uuid,$2::text,$3::integer)`, s.Incarnation, cursor, limit).Scan(&raw); err != nil {
		return CleanupPage{}, databaseError(err)
	}
	var page CleanupPage
	if json.Unmarshal(raw, &page) != nil || len(page.Items) > 64 {
		return CleanupPage{}, ErrUnavailable
	}
	return page, nil
}

// CleanupMonitor never retries an uncertain commit. Its next call only inspects
// remaining historical facts; neither path can reissue a GET or physical slot.
func (s Store) CleanupMonitor(ctx context.Context, h CleanupHint, c CleanupCursor, limit int) (CleanupOutcome, error) {
	return s.cleanup(ctx, `SELECT scheduler_private.cleanup_monitor($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::timestamptz,$6::uuid,$7::integer)`,
		s.Incarnation, h.WorkspaceID, h.ProjectID, h.MonitorID, c.Finished, c.RunID, limit)
}
func (s Store) CleanupTargetKey(ctx context.Context, kind, key string) (CleanupOutcome, error) {
	return s.cleanup(ctx, `SELECT scheduler_private.cleanup_target_key($1::uuid,$2::text,$3::text)`, s.Incarnation, kind, key)
}
func (s Store) cleanup(ctx context.Context, sql string, args ...any) (CleanupOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return CleanupOutcome{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	if err = tx.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
		return CleanupOutcome{}, databaseError(err)
	}
	var out CleanupOutcome
	if json.Unmarshal(raw, &out) != nil || out.RowsWritten < 0 || out.RowsWritten > 100 || out.DeletedRuns < 0 || out.DeletedRuns > 8 || out.DeletedKeys < 0 || out.DeletedKeys > 1 {
		return CleanupOutcome{}, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return CleanupOutcome{}, ErrCommitUnknown
	}
	return out, nil
}
