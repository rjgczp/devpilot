//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
)

// Capacity history below is administrative, never-prepared test data: no Probe,
// network capability, signed receipt or HTTP result is fabricated. It exercises
// real row triggers at real count boundaries; physical proofs use the Linux suite.
func retentionHistory(t *testing.T, db *pgxpool.Pool, h schedulerClaimHint, n int) {
	t.Helper()
	tx, e := db.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(context.Background(), `SELECT scheduler_private.lock_parents($1::uuid,$2::uuid,$3::uuid,NULL,NULL,false);`, h.w, h.p, h.m); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(context.Background(), `SELECT scheduler_private.lock_budgets($1::uuid,$2::uuid,$3::uuid)`, h.w, h.p, h.m); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(context.Background(), `WITH t AS MATERIALIZED(SELECT clock_timestamp()-interval '1 day' n), base AS MATERIALIZED(SELECT COALESCE(max(fencing_token),0) f FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3)
 INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,finished_at,lease_owner,worker_incarnation,lease_until,attempt_state,physical_release_confirmed_at,stop_reason)
 SELECT $1,$2,$3,uuidv7(),$4::bigint,$5::bigint,$6::bigint,base.f+x,t.n,t.n,t.n+interval '45 seconds',session_user,uuidv7(),t.n+interval '45 seconds','expired',t.n+interval '45 seconds','lease_expired' FROM generate_series(1,$7::integer)x CROSS JOIN t CROSS JOIN base`, h.w, h.p, h.m, h.v, h.g, h.e, n); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(context.Background(), `UPDATE monitor_schedules SET last_fencing_token=(SELECT max(fencing_token) FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3) WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func retentionLedger(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	var raw []byte
	e := db.QueryRow(context.Background(), `WITH actual AS (SELECT 'workspace'::text kind,workspace_id::text budget_key,count(*) n FROM monitor_runs GROUP BY workspace_id
 UNION ALL SELECT 'monitor',workspace_id::text||'/'||project_id::text||'/'||monitor_id::text,count(*) FROM monitor_runs GROUP BY workspace_id,project_id,monitor_id)
 SELECT jsonb_build_object('global',(SELECT terminal_count FROM scheduler_budget_state WHERE kind='global'), 'rows',(SELECT count(*) FROM monitor_runs),
 'pending',(SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'), 'actual_pending',(SELECT count(*) FROM monitor_runs WHERE attempt_state IN ('leased','started') OR physical_release_confirmed_at IS NULL),
 'scope_mismatch',(SELECT count(*) FROM actual a FULL JOIN (SELECT * FROM scheduler_budget_state WHERE kind IN ('workspace','monitor'))b USING(kind,budget_key) WHERE b.budget_key IS NULL OR COALESCE(a.n,0)<>b.terminal_count))`).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	var x struct {
		Global, Rows, Pending int
		Actual                int `json:"actual_pending"`
		Mismatch              int `json:"scope_mismatch"`
	}
	if json.Unmarshal(raw, &x) != nil || x.Global != x.Rows || x.Pending != x.Actual || x.Mismatch != 0 {
		t.Fatal("capacity ledger mismatch", string(raw))
	}
	t.Log("atomic ledger", string(raw))
}

func retentionClone(t *testing.T, db *pgxpool.Pool, base schedulerClaimHint) schedulerClaimHint {
	t.Helper()
	h := base
	e := db.QueryRow(context.Background(), `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id)
 SELECT workspace_id,project_id,'capacity-history',url,created_by_user_id FROM http_monitors WHERE id=$1 RETURNING id::text`, base.m).Scan(&h.m)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(context.Background(), `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, h.w, h.p, h.m); e != nil {
		t.Fatal(e)
	}
	return h
}

// Nested names give the existing identity fixture distinct real account names.
func retentionFixture(t *testing.T, db *pgxpool.Pool, name string) (f memberFixture, m monitor.Public, h schedulerClaimHint) {
	t.Helper()
	if !t.Run(name, func(t *testing.T) { f, m, h = schedulerClaimFixtureSeconds(t, db, 3600) }) {
		t.Fatal("identity fixture failed")
	}
	return
}

func TestBudgetRetentionFirstClaimAndRollback(t *testing.T) {
	db := budgetDB(t)
	_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	if queryInt(t, db, `SELECT count(*) FROM scheduler_budget_state WHERE kind<>'global'`) != 0 {
		t.Fatal("first Claim fixture unexpectedly has budgets")
	}
	// A real explicit transaction rolls all newly initialized rows and Run back.
	tx, e := w.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	var raw []byte
	e = tx.QueryRow(context.Background(), schedulerClaimSQL, h.args(inc)...).Scan(&raw)
	budgetSQLState(t, e, "00000")
	if e = tx.Rollback(context.Background()); e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`)+queryInt(t, db, `SELECT count(*) FROM scheduler_reservations`)+queryInt(t, db, `SELECT count(*) FROM scheduler_budget_state WHERE kind<>'global'`) != 0 {
		t.Fatal("rolled back first Claim left capacity")
	}
	// Distinct sessions initialize unrelated Workspace budgets concurrently.
	_, _, h2 := retentionFixture(t, db, "second-workspace")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	p1, c1 := startSchedulerClaim(t, w, inc, h)
	p2, c2 := startSchedulerClaim(t, w2, i2, h2)
	t.Log("independent first-claim sessions", p1, p2)
	budgetSQLState(t, (<-c1).err, "00000")
	budgetSQLState(t, (<-c2).err, "00000")
	if queryInt(t, db, `SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'`) != 2 {
		t.Fatal("missing or duplicate pending")
	}
	retentionLedger(t, db)
}

func TestBudgetRetentionMonitorCapacityAndSafeWrites(t *testing.T) {
	db := budgetDB(t)
	f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
	retentionHistory(t, db, h, 99)
	retentionLastStorage(t, db, h, h)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	_, e := schedulerClaim(t, w, inc, h)
	budgetSQLState(t, e, "P0001")
	if !strings.Contains(e.Error(), "retention_capacity") {
		t.Fatal(e)
	}
	status := status74Read(t, db, m, f.ownerToken)
	if status.BlockedReason == nil || *status.BlockedReason != "retention_capacity" {
		t.Fatal("missing fixed capacity status", status)
	}
	// Actual HTTP writes remain available at a full Monitor; no capacity is refunded.
	server, client, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	a := activationRead(t, db, f.od, m)
	body := fmt.Sprintf(`{"desired_state":"disabled","configuration_version":%q}`, m.Version)
	code, _, raw := checkpointHTTP(t, client, server.URL, "PUT", ("/api/v1/workspaces/" + m.WorkspaceID + "/projects/" + m.ProjectID + "/monitors/" + m.ID + "/activation"), body, f.ownerToken, monitor.ActivationETag(a), nil)
	if code != 200 {
		t.Fatal("full capacity blocked Pause", code, string(raw))
	}
	code, _, raw = checkpointHTTP(t, client, server.URL, "PATCH", "/api/v1/workspaces/"+m.WorkspaceID+"/projects/"+m.ProjectID+"/monitors/"+m.ID, `{"name":"capacity-patch"}`, f.ownerToken, monitor.ETag(m), nil)
	if code != 200 {
		t.Fatal("full capacity blocked PATCH", code, string(raw))
	}
	retentionLedger(t, db)
}

func TestBudgetRetentionWorkspaceCapacity(t *testing.T) {
	db := budgetDB(t)
	_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
	// Exactly100 real Monitor histories under this Workspace, not a counter-only UPDATE.
	for i := 0; i < 100; i++ {
		n := 100
		if i == 99 {
			n = 99
		}
		retentionHistory(t, db, retentionClone(t, db, h), n)
	}
	retentionLastStorage(t, db, retentionClone(t, db, h), retentionClone(t, db, h))
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	_, e := schedulerClaim(t, w, inc, h)
	budgetSQLState(t, e, "P0001")
	if !strings.Contains(e.Error(), "retention_capacity") {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 10000 {
		t.Fatal("workspace overshoot")
	}
	retentionLedger(t, db)
}

func TestBudgetRetentionGlobalLastSlot(t *testing.T) {
	db := budgetDB(t)
	var base schedulerClaimHint
	for workspace := 0; workspace < 10; workspace++ {
		_, _, base = retentionFixture(t, db, fmt.Sprintf("history-workspace-%d", workspace))
		for m := 0; m < 100; m++ {
			n := 100
			if workspace == 9 && m == 99 {
				n = 99
			}
			retentionHistory(t, db, retentionClone(t, db, base), n)
		}
	}
	_, _, a := retentionFixture(t, db, "last-slot-a")
	_, _, b := retentionFixture(t, db, "last-slot-b")
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	barrier, e := db.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(context.Background())
	if _, e = barrier.Exec(context.Background(), `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	p1, c1 := startSchedulerClaim(t, w1, i1, a)
	waitActivationBlock(t, db, p1)
	p2, c2 := startSchedulerClaim(t, w2, i2, b)
	waitActivationBlock(t, db, p2)
	// Collect locks while waiting, print after barrier release (unchanged250ms limit).
	var locks []byte
	if e = db.QueryRow(context.Background(), `SELECT jsonb_agg(jsonb_build_object('pid',pid,'blockers',pg_blocking_pids(pid),'wait',wait_event)) FROM pg_stat_activity WHERE pid=ANY($1)`, []int32{int32(p1), int32(p2)}).Scan(&locks); e != nil {
		t.Fatal(e)
	}
	if e = barrier.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Log("global final-slot wait graph", string(locks))
	errors := []error{(<-c1).err, (<-c2).err}
	ok, full := 0, 0
	for _, e := range errors {
		if e == nil {
			ok++
		} else {
			budgetSQLState(t, e, "P0001")
			if !strings.Contains(e.Error(), "retention_capacity") {
				t.Fatal(e)
			}
			full++
		}
	}
	if ok != 1 || full != 1 {
		t.Fatal("last storage slot not atomic", errors)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 100000 {
		t.Fatal("global cap overshoot")
	}
	retentionLedger(t, db)
}

func TestBudgetRetentionPendingQuarantineAndLostACK(t *testing.T) {
	db := budgetDB(t)
	_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	// Commit is real; its response is deliberately discarded before reconnecting.
	tx, e := w.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	var raw []byte
	e = tx.QueryRow(context.Background(), schedulerClaimSQL, h.args(inc)...).Scan(&raw)
	budgetSQLState(t, e, "00000")
	if e = tx.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
	w.Reset()
	var reconciled []byte
	e = w.QueryRow(context.Background(), `SELECT scheduler_private.reconcile_claim($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid)`, inc, h.w, h.p, h.m, h.r).Scan(&reconciled)
	budgetSQLState(t, e, "00000")
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 1 {
		t.Fatal("reconcile duplicated row")
	}
	if _, e = schedulerClaim(t, w, inc, h); e == nil {
		t.Fatal("duplicate claim accepted")
	}
	supervisor, si := schedulerSupervisorLogin(t, db)
	ns := budgetPrepare(t, db, supervisor, si, inc, h)
	_, e = schedulerStart(t, w, inc, h)
	budgetSQLState(t, e, "00000")
	budgetConsume(t, supervisor, si, ns, h)
	controller, ci := budgetController(t, db)
	_, e = controller.Exec(context.Background(), `SELECT scheduler_private.recover_started($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,1,$9::uuid,$10::uuid,'worker_lost')`, ci, h.w, h.p, h.m, h.r, h.v, h.g, h.e, ns, si)
	budgetSQLState(t, e, "00000")
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE attempt_state='unknown' AND physical_release_confirmed_at IS NULL AND http_status IS NULL AND success IS NULL`) != 1 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 || queryInt(t, db, `SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'`) != 1 {
		t.Fatal("unknown returned unproved capacity")
	}
	_, e = db.Exec(context.Background(), `DELETE FROM monitor_runs WHERE run_id=$1`, h.r)
	budgetSQLState(t, e, "23514")
	retentionLedger(t, db)
}

func TestBudgetRetentionACLAndMigration(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	// Empty maintenance lifecycle; no real execution or capacity facts are deleted.
	_, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`)
	if e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down", true)
	goose(t, dir, "up", true)
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 9 {
		t.Fatal("not schema9")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='scheduler_private' AND p.proname='run_capacity_accounting' AND p.prosecdef AND pg_get_userbyid(p.proowner)='devpilot_scheduler_owner' AND 'search_path=pg_catalog, scheduler_private'=ANY(p.proconfig)`) != 1 {
		t.Fatal("trigger owner/path")
	}
	if queryInt(t, db, `SELECT count(*) FROM (VALUES('devpilot_scheduler_api'),('devpilot_scheduler_worker'),('devpilot_scheduler_controller'),('devpilot_scheduler_supervisor'))r(n) WHERE has_function_privilege(r.n,'scheduler_private.run_capacity_accounting()','EXECUTE')`) != 0 {
		t.Fatal("new runtime EXECUTE")
	}
	var read, write, oldWrite bool
	if e = db.QueryRow(ctx, `SELECT has_column_privilege('devpilot_scheduler_api_owner','scheduler_budget_state','retention_pending_count','SELECT'),has_column_privilege('devpilot_scheduler_api_owner','scheduler_budget_state','retention_pending_count','UPDATE'),has_column_privilege('devpilot_scheduler_api_owner','scheduler_budget_state','schedule_count','UPDATE')`).Scan(&read, &write, &oldWrite); e != nil || !read || write || !oldWrite {
		t.Fatal("new-column ACL", read, write, oldWrite, e)
	}
	_, e = db.Exec(ctx, `UPDATE scheduler_budget_state SET retention_pending_count=33 WHERE kind='global'`)
	budgetSQLState(t, e, "23514")
	_, e = db.Exec(ctx, `TRUNCATE monitor_runs`)
	budgetSQLState(t, e, "0A000") // Existing FK protection fires before row-table triggers.
	_, e = db.Exec(ctx, `TRUNCATE monitor_runs CASCADE`)
	budgetSQLState(t, e, "23514") // The new statement guard blocks the remaining route.
	retentionLedger(t, db)
}

// Administrative terminal-history writers exercise the Trigger's last record
// slot, independently of Claim's physical per-Monitor singleton. Both use the
// same parent/budget lock protocol; they create no execution authority.
func retentionLastStorage(t *testing.T, db *pgxpool.Pool, a, b schedulerClaimHint) {
	t.Helper()
	ctx := context.Background()
	barrier, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(ctx)
	if _, e = barrier.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	type result struct{ err error }
	start := func(h schedulerClaimHint) (int, <-chan result) {
		conn, e := db.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		pid := int(conn.Conn().PgConn().PID())
		ch := make(chan result, 1)
		go func() {
			defer conn.Release()
			run := func() error {
				tx, e := conn.Begin(ctx)
				if e != nil {
					return e
				}
				defer tx.Rollback(ctx)
				if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='250ms'; SET LOCAL statement_timeout='2s'; SET LOCAL transaction_timeout='2s'`); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `SELECT scheduler_private.lock_parents($1::uuid,$2::uuid,$3::uuid,NULL,NULL,false)`, h.w, h.p, h.m); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `SELECT scheduler_private.lock_budgets($1::uuid,$2::uuid,$3::uuid)`, h.w, h.p, h.m); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp()-interval '1 day' n),base AS MATERIALIZED(SELECT COALESCE(max(fencing_token),0)+1 f FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3)
   INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,finished_at,lease_owner,worker_incarnation,lease_until,attempt_state,physical_release_confirmed_at,stop_reason)
   SELECT $1,$2,$3,uuidv7(),$4::bigint,$5::bigint,$6::bigint,f,n,n,n+interval '45 seconds',session_user,uuidv7(),n+interval '45 seconds','expired',n+interval '45 seconds','lease_expired' FROM t,base`, h.w, h.p, h.m, h.v, h.g, h.e); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `UPDATE monitor_schedules SET last_fencing_token=(SELECT max(fencing_token) FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3) WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m); e != nil {
					return e
				}
				return tx.Commit(ctx)
			}
			ch <- result{run()}
		}()
		return pid, ch
	}
	p1, c1 := start(a)
	waitActivationBlock(t, db, p1)
	p2, c2 := start(b)
	waitActivationBlock(t, db, p2)
	var graph []byte
	if e = db.QueryRow(ctx, `SELECT jsonb_build_object('locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l WHERE pid=ANY($1)), 'waits',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'blockers',pg_blocking_pids(pid))) FROM pg_stat_activity WHERE pid=ANY($1)))`, []int32{int32(p1), int32(p2)}).Scan(&graph); e != nil {
		t.Fatal(e)
	}
	if e = barrier.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log("scope final-slot lock graph", string(graph))
	one, two := (<-c1).err, (<-c2).err
	budgetSQLState(t, one, "00000")
	budgetSQLState(t, two, "P0001")
	if !strings.Contains(two.Error(), "retention_capacity") {
		t.Fatal(two)
	}
}

func TestBudgetRetentionUpgradeInFlight(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down", true)
	_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	_, e := schedulerClaim(t, w, inc, h)
	budgetSQLState(t, e, "00000")
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "up", true)
	goose(t, dir, "up", true)
	retentionLedger(t, db)
	if queryInt(t, db, `SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'`) != 1 {
		t.Fatal("upgrade lost in-flight capacity")
	}
	out := goose(t, dir, "down", false)
	if !strings.Contains(string(out), "no retained runs") || queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 9 {
		t.Fatal("unsafe downgrade changed capacity", string(out))
	}
	retentionLedger(t, db)
}

func TestBudgetRetentionSevenDayHistoryIsNotFree(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down", true)
	f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
	// Administrative schema8 import of an old, never-prepared expiry. There is
	// no Supervisor receipt and no physical execution/release claim in this seed.
	tx, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT scheduler_private.lock_parents($1::uuid,$2::uuid,$3::uuid,NULL,NULL,false)`, h.w, h.p, h.m); e != nil {
		t.Fatal(e)
	}
	// Import already-cold keys at their original time; never shorten a live key
	// to make a DELETE test pass. The global row is locked before their creation.
	if _, e = tx.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,last_refill_at,keep_until)
 VALUES('workspace',$1,clock_timestamp()-interval '8 days',clock_timestamp()-interval '7 days'),
 ('project',$2,clock_timestamp()-interval '8 days',clock_timestamp()-interval '7 days'),
 ('monitor',$3,clock_timestamp()-interval '8 days',clock_timestamp()-interval '7 days')`, h.w, h.w+"/"+h.p, h.w+"/"+h.p+"/"+h.m); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SELECT scheduler_private.lock_budgets($1::uuid,$2::uuid,$3::uuid)`, h.w, h.p, h.m); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `WITH t AS (SELECT clock_timestamp()-interval '8 days' n)
 INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,finished_at,lease_owner,worker_incarnation,lease_until,attempt_state,physical_release_confirmed_at,stop_reason)
 SELECT $1,$2,$3,uuidv7(),$4::bigint,$5::bigint,$6::bigint,1,n,n,n+interval '45 seconds',session_user,uuidv7(),n+interval '45 seconds','expired',n+interval '45 seconds','lease_expired' FROM t`, h.w, h.p, h.m, h.v, h.g, h.e); e != nil {
		t.Fatal(e)
	}
	// Schema8's accounting is inside Claim, so this import must include its exact
	// one-row storage ledger; it does not set a counter to an artificial cap.
	if _, e = tx.Exec(ctx, `UPDATE scheduler_budget_state SET terminal_count=terminal_count+1 WHERE kind='global' OR (kind='workspace' AND budget_key=$1) OR (kind='monitor' AND budget_key=$2)`, h.w, h.w+"/"+h.p+"/"+h.m); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE monitor_schedules SET last_fencing_token=1 WHERE monitor_id=$1`, h.m); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "up", true)
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	_, e = schedulerClaim(t, w, inc, h)
	budgetSQLState(t, e, "P0001")
	if !strings.Contains(e.Error(), "retention_capacity") || queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 1 {
		t.Fatal("expired history was treated as free capacity", e)
	}
	api := status74API(t, db)
	s := status74Read(t, api, m, f.ownerToken)
	if s.BlockedReason == nil || *s.BlockedReason != "retention_capacity" {
		t.Fatal("expiry capacity missing", s)
	}
	retentionLedger(t, db)
	// Exercise the foundation's single-row DELETE transaction, not a cleanup
	// service/keyset/pointer algorithm. Active authorization first blocks it.
	_, e = db.Exec(ctx, `DELETE FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m)
	budgetSQLState(t, e, "23514")
	a := activationRead(t, db, f.od, m)
	if _, e = activationPut(monitor.Store{Pool: db}, f.od, m, a, "disabled"); e != nil {
		t.Fatal(e)
	}
	for _, commit := range []bool{false, true} {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT scheduler_private.lock_parents($1::uuid,$2::uuid,$3::uuid,NULL,NULL,false)`, h.w, h.p, h.m); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `SELECT run_id FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 FOR UPDATE`, h.w, h.p, h.m); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `SELECT scheduler_private.lock_budgets($1::uuid,$2::uuid,$3::uuid)`, h.w, h.p, h.m); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `SELECT reservation_id FROM scheduler_reservations WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 FOR UPDATE`, h.w, h.p, h.m); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m); err != nil {
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit(ctx)
		} else {
			err = tx.Rollback(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if commit {
			want = 0
		}
		if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != want {
			t.Fatal("DELETE record/ledger transaction not atomic")
		}
		retentionLedger(t, db)
	}
}
