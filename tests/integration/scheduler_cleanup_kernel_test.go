//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture && recoveryfixture

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

func cleanupPhysicalDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("CLEANUP82_PRESERVED_CONTROL") != "real-recovery-pause-v1" {
		t.Fatal("requires retained independent physical fixture, no offline replacement")
	}
	db := openDB(t)
	var cluster string
	if e := db.QueryRow(context.Background(), `SHOW cluster_name`).Scan(&cluster); e != nil || cluster != "devpilot-budget-disposable" {
		t.Fatal("disposable PG18 required", e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE attempt_state='unknown' AND physical_release_confirmed_at IS NOT NULL AND success IS NULL`) != 1 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE reservation_state='released' AND release_basis='supervisor' AND termination_receipt_id IS NOT NULL`) != 1 {
		t.Fatal("real recovery/physical proof missing")
	}
	return db
}

// Both phase tests stay under the existing120s process budget. The orchestration
// waits the genuine one-hour cooldown OUTSIDE any Go test, connection or txn.
func TestCleanupPhysicalPreparePG(t *testing.T) {
	db := cleanupPhysicalDB(t)
	ctx := context.Background()
	var h schedulerClaimHint
	if e := db.QueryRow(ctx, `SELECT workspace_id::text,project_id::text,monitor_id::text,run_id::text,configuration_version::text,activation_generation::text,scheduler_epoch::text FROM monitor_runs`).Scan(&h.w, &h.p, &h.m, &h.r, &h.v, &h.g, &h.e); e != nil {
		t.Fatal(e)
	}
	// Administrative never-prepared expiry rows reserve actual capacity. They do
	// not replace or modify the real unknown Run or its signed physical receipt.
	retentionHistory(t, db, h, 99)
	c, ci := budgetController(t, db)
	before := recoveryBudget(t, db)
	out := cleanupCall(t, c, ci, h, 8)
	if out.DeletedRuns != 0 || out.Reason != "protected" {
		t.Fatal("fresh physical release bypassed cooling", out)
	}
	after := recoveryBudget(t, db)
	if string(before) != string(after) {
		t.Fatal("protected cleanup changed resource ledger")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 100 {
		t.Fatal("missing actual full retained capacity")
	}
	retentionLedger(t, db)
	var seconds float64
	var until time.Time
	if e := db.QueryRow(ctx, `SELECT max(keep_until),GREATEST(0,extract(epoch FROM max(keep_until)-clock_timestamp()))::float8 FROM scheduler_budget_state WHERE kind<>'global'`).Scan(&until, &seconds); e != nil || seconds < 0 || seconds > 3601 {
		t.Fatal("invalid real cooldown", seconds, e)
	}
	raw, _ := json.Marshal(map[string]any{"until": until, "wait_seconds": seconds, "run_id": h.r})
	fmt.Println("COOLING82 " + string(raw))
}

func TestCleanupPhysicalFinishPG(t *testing.T) {
	db := cleanupPhysicalDB(t)
	ctx := context.Background()
	var h schedulerClaimHint
	var finished time.Time
	var ns, si string
	var fence string
	if e := db.QueryRow(ctx, `SELECT r.workspace_id::text,r.project_id::text,r.monitor_id::text,r.run_id::text,r.configuration_version::text,r.activation_generation::text,r.scheduler_epoch::text,r.fencing_token::text,r.finished_at,q.namespace_incarnation::text,q.supervisor_incarnation::text FROM monitor_runs r JOIN scheduler_reservations q USING(workspace_id,project_id,monitor_id,run_id) WHERE r.attempt_state='unknown'`).Scan(&h.w, &h.p, &h.m, &h.r, &h.v, &h.g, &h.e, &fence, &finished, &ns, &si); e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM scheduler_budget_state WHERE keep_until>clock_timestamp() AND kind<>'global'`) != 0 {
		t.Fatal("real cooldown not finished")
	}
	c, ci := budgetController(t, db)
	var origin, ip string
	if e := db.QueryRow(ctx, `SELECT origin_key,ip_key FROM scheduler_reservations WHERE run_id=$1`, h.r).Scan(&origin, &ip); e != nil {
		t.Fatal(e)
	}
	control := scheduler.Store{DB: c, Incarnation: ci}
	for kind, key := range map[string]string{"origin": origin, "ip": ip} {
		held, e := control.CleanupTargetKey(ctx, kind, key)
		if e != nil || held.DeletedKeys != 0 {
			t.Fatal("released but retained Reservation lost target key", held, e)
		}
	}
	var slots, pending int
	if e := db.QueryRow(ctx, `SELECT in_use,retention_pending_count FROM scheduler_budget_state WHERE kind='global'`).Scan(&slots, &pending); e != nil || slots != 0 || pending != 0 {
		t.Fatal("physical resources were not independently released", e)
	}
	cursorTime := finished.Add(-time.Microsecond)
	zero := "00000000-0000-0000-0000-000000000000"
	out, e := (scheduler.Store{DB: c, Incarnation: ci}).CleanupMonitor(ctx, scheduler.CleanupHint{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m}, scheduler.CleanupCursor{Finished: &cursorTime, RunID: &zero}, 8)
	if e != nil || out.DeletedRuns != 1 || out.RowsWritten != 7 {
		t.Fatal("real unknown cleanup failed", out, e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1`, h.r) != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1`, h.r) != 0 {
		t.Fatal("retired Run still referenced")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND last_attempt_run_id IS NULL AND latest_result_run_id IS NULL AND last_fencing_token=100`, h.m) != 1 {
		t.Fatal("unknown retirement resurrected projection or reset Fence")
	}
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != slots || queryInt(t, db, `SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'`) != pending {
		t.Fatal("cleanup changed physical slots/pending")
	}
	retentionLedger(t, db)
	for kind, key := range map[string]string{"origin": origin, "ip": ip} {
		freed, e := control.CleanupTargetKey(ctx, kind, key)
		if e != nil || freed.DeletedKeys != 1 || freed.RowsWritten != 3 {
			t.Fatal("unreferenced cold key not retired atomically", freed, e)
		}
	}
	keyCount := queryInt(t, db, `SELECT key_count FROM scheduler_budget_state WHERE kind='global'`)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	_, e = w.Exec(ctx, `SELECT scheduler_private.commit_result($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,uuidv7(),1,200,true,NULL)`, wi, h.w, h.p, h.m, h.r, h.v, h.g, h.e, fence, ns, si)
	budgetSQLState(t, e, "P0001")
	_, e = w.Exec(ctx, `SELECT scheduler_private.reserve_target($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,$8::bigint,$9::bigint,$10,$11)`, wi, h.w, h.p, h.m, h.r, ns, si, h.e, fence, origin, ip)
	budgetSQLState(t, e, "P0001")
	sup, supInc := schedulerSupervisorLogin(t, db)
	receipt, err := os.ReadFile("/retired-proof/old.receipt")
	if err != nil {
		t.Fatal(err)
	}
	_, e = sup.Exec(ctx, `SELECT scheduler_private.accept_termination($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bytea)`, supInc, h.w, h.p, h.m, h.r, receipt)
	if e == nil {
		t.Fatal("old physical receipt released retired identity again")
	}
	t.Log("retired receipt rejected", e)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 99 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 0 {
		t.Fatal("replay changed retained/physical resources")
	}
	if queryInt(t, db, `SELECT key_count FROM scheduler_budget_state WHERE kind='global'`) != keyCount {
		t.Fatal("old permit replay recreated retired keys")
	}
}

func TestCleanupPhysicalRejectPG(t *testing.T) {
	if os.Getenv("CLEANUP82_PRESERVED_CONTROL") != "real-recovery-unconfirmed-v1" {
		t.Fatal("requires real unconfirmed Supervisor fixture")
	}
	db := openDB(t)
	ctx := context.Background()
	var h schedulerClaimHint
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE attempt_state='unknown' AND physical_release_confirmed_at IS NULL`) != 1 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE reservation_state='quarantined' AND termination_receipt IS NULL`) != 1 {
		t.Fatal("missing actual unconfirmed physical state")
	}
	if e := db.QueryRow(ctx, `SELECT workspace_id::text,project_id::text,monitor_id::text FROM monitor_runs`).Scan(&h.w, &h.p, &h.m); e != nil {
		t.Fatal(e)
	}
	c, ci := budgetController(t, db)
	before := recoveryBudget(t, db)
	out := cleanupCall(t, c, ci, h, 8)
	if out.DeletedRuns != 0 || recoveryBudget(t, db) != before {
		t.Fatal("unconfirmed stop reclaimed resources", out)
	}
	_, e := db.Exec(ctx, `DELETE FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m)
	budgetSQLState(t, e, "23514")
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 || queryInt(t, db, `SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'`) != 1 {
		t.Fatal("quarantine lost physical/pending slot")
	}
	retentionLedger(t, db)
}
