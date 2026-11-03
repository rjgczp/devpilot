//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type cleanupLedgerReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func cleanupMissingLedgerSnapshot(t *testing.T, db cleanupLedgerReader) string {
	t.Helper()
	var raw []byte
	err := db.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'runs',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY run_id) FROM monitor_runs r),'[]'),
 'reservations',COALESCE((SELECT jsonb_agg(to_jsonb(q) ORDER BY run_id) FROM scheduler_reservations q),'[]'),
 'schedules',COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY monitor_id) FROM monitor_schedules s),'[]'),
 'budgets',COALESCE((SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b),'[]'),
 'control',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',control_id,'gate_at',last_gate_at,'transaction',last_transaction) ORDER BY control_id) FROM scheduler_control),'[]'))`).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func cleanupMissingLedger(t *testing.T, path string) {
	n := 2
	if path == "empty-registration" {
		n = 0
	}
	db, h := cleanupHistory(t, n)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	if n > 0 {
		// A real historical attempt pointer; never manufacture an HTTP result.
		_, err := db.Exec(ctx, `UPDATE monitor_schedules SET last_attempt_run_id=(SELECT run_id FROM monitor_runs ORDER BY fencing_token DESC LIMIT 1)`)
		if err != nil {
			t.Fatal(err)
		}
	}
	baseline := cleanupMissingLedgerSnapshot(t, db)
	bad := 0
	for _, kind := range []string{"global", "workspace", "project", "monitor"} {
		key := map[string]string{"global": "global", "workspace": h.w, "project": h.w + "/" + h.p, "monitor": h.w + "/" + h.p + "/" + h.m}[kind]
		var saved []byte
		if err := db.QueryRow(ctx, `SELECT to_jsonb(b) FROM scheduler_budget_state b WHERE kind=$1 AND budget_key=$2`, kind, key).Scan(&saved); err != nil {
			t.Fatal(err)
		}
		// First prove ordinary constraints with all Trigger protection enabled.
		trial, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = trial.Exec(ctx, `DELETE FROM scheduler_budget_state WHERE kind=$1 AND budget_key=$2`, kind, key)
		want := "00000"
		injected := n > 0 || kind == "global"
		if injected {
			want = "23514"
		}
		budgetSQLState(t, err, want)
		if err = trial.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		// Only the pre-existing disposable fixture administrator injects damage.
		// SET LOCAL ends BEFORE the real restricted Controller call. It neither
		// invents physical proof nor changes any Run/Reservation/capacity value.
		fault, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if injected {
			if _, err = fault.Exec(ctx, `SET LOCAL session_replication_role=replica`); err != nil {
				t.Fatal(err)
			}
		}
		tag, err := fault.Exec(ctx, `DELETE FROM scheduler_budget_state WHERE kind=$1 AND budget_key=$2`, kind, key)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal(tag, err)
		}
		if err = fault.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		before := cleanupMissingLedgerSnapshot(t, db)
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		tx, err := c.BeginTx(callCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		var identity []byte
		err = tx.QueryRow(callCtx, `SELECT jsonb_build_object('session_user',session_user,'current_user',current_user,
 'replication_role',current_setting('session_replication_role'),'pid',pg_backend_pid(),'xid',pg_current_xact_id(),
 'triggers',(SELECT jsonb_agg(jsonb_build_object('name',tgname,'enabled',tgenabled) ORDER BY tgname)
 FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN ('monitor_runs'::regclass,'monitor_schedules'::regclass,'scheduler_budget_state'::regclass)))`).Scan(&identity)
		if err != nil {
			t.Fatal(err)
		}
		var guard struct {
			Session     string `json:"session_user"`
			Current     string `json:"current_user"`
			Replication string `json:"replication_role"`
			Triggers    []struct {
				Enabled string `json:"enabled"`
			} `json:"triggers"`
		}
		if json.Unmarshal(identity, &guard) != nil || guard.Session != "scheduler_budget_controller" || guard.Current != guard.Session || guard.Replication != "origin" || len(guard.Triggers) == 0 {
			t.Fatal("restricted caller or Trigger context changed", string(identity))
		}
		for _, trigger := range guard.Triggers {
			if trigger.Enabled != "O" {
				t.Fatal("cleanup ran with disabled Trigger", string(identity))
			}
		}
		t.Logf("case=%s/%s controlled_injection=%v identity=%s before=%s", path, kind, injected, identity, before)
		var afterFinished any
		var afterRun any
		if path == "no-candidate-run" {
			afterFinished = time.Now().UTC()
			afterRun = "00000000-0000-0000-0000-000000000001"
		}
		var raw []byte
		err = tx.QueryRow(callCtx, cleanupSQL, ci, h.w, h.p, h.m, afterFinished, afterRun, 8).Scan(&raw)
		state := "00000"
		if err != nil {
			p, ok := err.(*pgconn.PgError)
			if !ok {
				t.Fatal(err)
			}
			state = p.Code
		} else {
			t.Logf("UNEXPECTED SUCCESS case=%s/%s response=%s; diagnostic rolls back instead of committing", path, kind, raw)
		}
		t.Logf("case=%s/%s SQLSTATE=%s error=%v", path, kind, state, err)
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
		if after := cleanupMissingLedgerSnapshot(t, db); before != after {
			t.Fatal("failed/rolled-back cleanup changed protected state", before, after)
		}
		if state != "23514" {
			bad++
		}
		// Restore only the exact faulted row in this disposable test, so each
		// case starts from the same ledger. No physical state or counters edited.
		restore, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if injected {
			if _, err = restore.Exec(ctx, `SET LOCAL session_replication_role=replica`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = restore.Exec(ctx, `INSERT INTO scheduler_budget_state SELECT * FROM jsonb_populate_record(NULL::scheduler_budget_state,$1::jsonb)`, saved); err != nil {
			t.Fatal(err)
		}
		if err = restore.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if cleanupMissingLedgerSnapshot(t, db) != baseline {
			t.Fatal("controlled fault restoration changed baseline")
		}
	}
	if bad != 0 {
		t.Fatalf("%d missing-ledger cases did not fail closed", bad)
	}
}

func TestBudgetCleanupMissingLedgerRuns(t *testing.T) { cleanupMissingLedger(t, "eligible-run") }
func TestBudgetCleanupMissingLedgerNoCandidate(t *testing.T) {
	cleanupMissingLedger(t, "no-candidate-run")
}
func TestBudgetCleanupMissingLedgerRegistration(t *testing.T) {
	cleanupMissingLedger(t, "empty-registration")
}
