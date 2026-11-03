//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

const cleanupSQL = `SELECT scheduler_private.cleanup_monitor($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::timestamptz,$6::uuid,$7::integer)`

// Administrative schema8 import of never-prepared historical expiries. No
// Supervisor receipt, prepared namespace or HTTP result is invented here.
// As in task8.1's schema8 import, original cold timestamps are inserted once,
// never shortened. M9 checks the exact ledger on upgrade; M10 then cleans it.
func cleanupHistory(t *testing.T, n int) (*pgxpool.Pool, schedulerClaimHint) {
	t.Helper()
	db := budgetDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down-to 8", true)
	f, m := monitorFixture(t, db)
	h := schedulerClaimHint{w: m.WorkspaceID, p: m.ProjectID, m: m.ID, u: f.owner.ID, v: m.Version, g: "1", e: "1"}
	if _, e := db.Exec(ctx, `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id,last_fencing_token) VALUES($1,$2,$3,$4)`, h.w, h.p, h.m, n); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,last_refill_at,keep_until,terminal_count)
 VALUES('workspace',$1,2000000,clock_timestamp()-interval '8 days',clock_timestamp()-interval '7 days',$4),
 ('project',$1||'/'||$2,1000000,clock_timestamp()-interval '8 days',clock_timestamp()-interval '7 days',0),
 ('monitor',$1||'/'||$2||'/'||$3,1000000,clock_timestamp()-interval '8 days',clock_timestamp()-interval '7 days',$4)`, h.w, h.p, h.m, n); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(ctx, `WITH stamp AS(SELECT clock_timestamp()-interval '8 days' n)
 INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,
 scheduled_at,claimed_at,finished_at,lease_owner,worker_incarnation,lease_until,attempt_state,physical_release_confirmed_at,stop_reason)
 SELECT $1,$2,$3,uuidv7(),1,1,1,i,n,n,n+interval '45 seconds',session_user,uuidv7(),n+interval '45 seconds','expired',n+interval '45 seconds','lease_expired'
 FROM stamp,generate_series(1,$4::integer)i`, h.w, h.p, h.m, n); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(ctx, `INSERT INTO scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state,release_basis,released_at)
 SELECT workspace_id,project_id,monitor_id,run_id,'released','never_prepared',physical_release_confirmed_at FROM monitor_runs;
 UPDATE scheduler_budget_state SET terminal_count=(SELECT count(*) FROM monitor_runs) WHERE kind='global'`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "up", true)
	retentionLedger(t, db)
	return db, h
}
func cleanupCall(t *testing.T, c *pgxpool.Pool, inc string, h schedulerClaimHint, limit int) scheduler.CleanupOutcome {
	t.Helper()
	o, e := (scheduler.Store{DB: c, Incarnation: inc}).CleanupMonitor(context.Background(), scheduler.CleanupHint{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m}, scheduler.CleanupCursor{}, limit)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("cleanup outcome %+v", o)
	return o
}

func TestBudgetCleanupKeysetLedgerAndRetirement(t *testing.T) {
	db, h := cleanupHistory(t, 17)
	c, ci := budgetController(t, db)
	store := scheduler.Store{DB: c, Incarnation: ci}
	page, e := store.RetentionCandidates(context.Background(), nil, 1)
	if e != nil || len(page.Items) != 1 || page.Items[0].MonitorID != h.m || page.NextCursor == nil {
		t.Fatal(page, e)
	}
	var cursor scheduler.CleanupCursor
	total := 0
	for batch := 0; batch < 3; batch++ {
		out, e := store.CleanupMonitor(context.Background(), page.Items[0], cursor, 8)
		if e != nil || out.DeletedRuns < 1 || out.DeletedRuns > 8 || out.RowsWritten > 100 || out.RowsWritten < out.DeletedRuns*5 {
			t.Fatal(out, e)
		}
		want := []int{41, 41, 14}[batch]
		if out.RowsWritten != want {
			t.Fatalf("transaction-local write count: got %d want %d", out.RowsWritten, want)
		}
		total += out.DeletedRuns
		cursor = out.CleanupCursor
		retentionLedger(t, db)
		t.Log("committed cleanup batch", batch, out)
	}
	if total != 17 || queryInt(t, db, `SELECT count(*) FROM monitor_schedules`) != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations`) != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_budget_state WHERE kind<>'global'`) != 0 {
		t.Fatal("retirement left references", total)
	}
	// An old deleted scope is unavailable; it cannot create a new schedule/Run.
	_, e = c.Exec(context.Background(), cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8)
	budgetSQLState(t, e, "P0001")
	if queryInt(t, db, `SELECT key_count FROM scheduler_budget_state WHERE kind='global'`) != 1 {
		t.Fatal("key accounting")
	}
}

func TestBudgetCleanupRollbackACKAndLastSlot(t *testing.T) {
	db, h := cleanupHistory(t, 9)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	tx, e := c.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var out scheduler.CleanupOutcome
	var raw []byte
	e = tx.QueryRow(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8).Scan(&raw)
	budgetSQLState(t, e, "00000")
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 9 {
		t.Fatal("rollback lost history")
	}
	retentionLedger(t, db)
	tx, e = c.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	e = tx.QueryRow(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8).Scan(&raw)
	budgetSQLState(t, e, "00000")
	// Real COMMIT succeeds; deliberately discard the application acknowledgement.
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	c.Reset()
	if json.Unmarshal(raw, &out) != nil || out.DeletedRuns != 8 {
		t.Fatal(string(raw))
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 1 {
		t.Fatal("unknown ACK changed committed fact")
	}
	result := cleanupCall(t, c, ci, h, 8)
	if result.DeletedRuns != 1 {
		t.Fatal("retry duplicated deletion", result)
	}
	retentionLedger(t, db)
}

func TestBudgetCleanupFullTransactionWriteBound(t *testing.T) {
	db, h := cleanupHistory(t, 9)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	// The owner fixture observes the PostgreSQL transactional row statistics;
	// runtime Controller still has no table DML. Trigger updates count as writes.
	tx, e := c.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var raw []byte
	e = tx.QueryRow(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8).Scan(&raw)
	budgetSQLState(t, e, "00000")
	_, e = tx.Exec(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8)
	budgetSQLState(t, e, "25000")
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 9 {
		t.Fatal("second call did not rollback entire transaction")
	}
	retentionLedger(t, db)
}

func TestBudgetCleanupTargetCoolingReferencesAndACL(t *testing.T) {
	db, h := cleanupHistory(t, 1)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	cold := strings.Repeat("12", 32)
	hot := strings.Repeat("23", 32)
	if _, e := db.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,last_refill_at,keep_until)
 VALUES('origin',$1,1000000,clock_timestamp()-interval '2 hours',clock_timestamp()-interval '1 hour'),
 ('ip',$2,1000000,clock_timestamp(),clock_timestamp()+interval '1 hour')`, cold, hot); e != nil {
		t.Fatal(e)
	}
	s := scheduler.Store{DB: c, Incarnation: ci}
	out, e := s.CleanupTargetKey(ctx, "origin", cold)
	if e != nil || out.DeletedKeys != 1 || out.RowsWritten != 3 {
		t.Fatal(out, e)
	}
	out, e = s.CleanupTargetKey(ctx, "origin", cold)
	if e != nil || out.DeletedKeys != 0 {
		t.Fatal(out, e)
	}
	out, e = s.CleanupTargetKey(ctx, "ip", hot)
	if e != nil || out.DeletedKeys != 0 {
		t.Fatal("live cooling removed", out, e)
	}
	signatures := []string{"scheduler_private.retention_candidates(uuid,text,integer)", "scheduler_private.cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer)", "scheduler_private.cleanup_target_key(uuid,text,text)"}
	for _, sig := range signatures {
		var granted int
		if e = db.QueryRow(ctx, `SELECT count(*) FROM (VALUES('devpilot_scheduler_worker'),('devpilot_scheduler_supervisor'),('devpilot_scheduler_api'))r(n) WHERE has_function_privilege(r.n,$1,'EXECUTE')`, sig).Scan(&granted); e != nil || granted != 0 {
			t.Fatal("unexpected runtime EXECUTE", e)
		}
	}
	_, e = c.Exec(ctx, `DELETE FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m)
	budgetSQLState(t, e, "42501")
	_, e = c.Exec(ctx, `SELECT scheduler_private.cleanup_target_key($1::uuid,'global','global')`, ci)
	budgetSQLState(t, e, "22023")
}

func TestBudgetCleanupScopeAndIdentityReject(t *testing.T) {
	db, h := cleanupHistory(t, 1)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	for _, args := range [][]any{{ci, h.w, h.p, h.u, nil, nil, 8}, {ci, h.w, h.p, h.m, nil, nil, 9}, {h.u, h.w, h.p, h.m, nil, nil, 8}} {
		_, e := c.Exec(ctx, cleanupSQL, args...)
		if e == nil {
			t.Fatal("invalid scope/limit/identity accepted")
		}
		t.Log("fixed rejection", e)
	}
	for _, cursor := range []string{`[]`, `{"phase":"other"}`, strings.Repeat("x", 513)} {
		_, e := c.Exec(ctx, `SELECT scheduler_private.retention_candidates($1::uuid,$2,64)`, ci, cursor)
		if e == nil {
			t.Fatal("bad cursor accepted")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 1 {
		t.Fatal("rejection deleted data")
	}
	retentionLedger(t, db)
}

func TestBudgetCleanupParentWaitAndFreshIdentity(t *testing.T) {
	db, h := cleanupHistory(t, 2)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	barrier, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(ctx)
	if _, e = barrier.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, h.w); e != nil {
		t.Fatal(e)
	}
	conn, e := c.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	pid := int(conn.Conn().PgConn().PID())
	done := make(chan error, 1)
	go func() { _, err := conn.Exec(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8); done <- err }()
	waitActivationBlock(t, db, pid)
	var graph []byte
	if e = db.QueryRow(ctx, `SELECT jsonb_build_object('at',clock_timestamp(),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l WHERE pid=$1),'blockers',pg_blocking_pids($1))`, pid).Scan(&graph); e != nil {
		t.Fatal(e)
	}
	if _, e = barrier.Exec(ctx, `UPDATE scheduler_control SET emergency_stop=true WHERE kind='controller' AND incarnation=$1`, ci); e != nil {
		t.Fatal(e)
	}
	if e = barrier.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log("fresh identity barrier", string(graph))
	budgetSQLState(t, <-done, "42501")
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 2 {
		t.Fatal("wait used stale identity")
	}
}

func TestBudgetCleanupConservativeProtection(t *testing.T) {
	db, h := cleanupHistory(t, 1)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	// Administrative authorization-shaped row exercises the unchanged M9 guard;
	// this setup does not claim an Owner command, Probe or physical release.
	if _, e := db.Exec(ctx, `UPDATE monitor_schedules SET authorized_configuration_version=1,authorized_activation_generation=1,consent_protocol=1,
 issued_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',scheduler_epoch=scheduler_epoch+1 WHERE monitor_id=$1`, h.m); e != nil {
		t.Fatal(e)
	}
	out := cleanupCall(t, c, ci, h, 8)
	if out.DeletedRuns != 0 || out.Reason != "protected" {
		t.Fatal(out)
	}
	if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations`) != 1 {
		t.Fatal("protected receipt retired")
	}
	retentionLedger(t, db)
	// Transaction limit is unchanged in the real LOGIN configuration.
	var lock, txn string
	if e := c.QueryRow(ctx, `SELECT current_setting('lock_timeout'),current_setting('transaction_timeout')`).Scan(&lock, &txn); e != nil || lock != "250ms" || txn != "2s" {
		t.Fatal(lock, txn, e)
	}
}

func TestBudgetCleanupWholeTransactionTriggerWrites(t *testing.T) {
	db, h := cleanupHistory(t, 8)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	// A trusted provisioning fixture supplies49 additional expired incarnations
	// of the same SQL login. It gives no new role/table privilege.
	if _, e := db.Exec(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,expires_at,emergency_stop)
 SELECT 'old-cleanup-'||i,'controller','scheduler_budget_controller',uuidv7(),clock_timestamp()-interval '1 day',true FROM generate_series(1,49)i;
 UPDATE monitor_schedules SET last_attempt_run_id=(SELECT run_id FROM monitor_runs ORDER BY run_id LIMIT 1)`); e != nil {
		t.Fatal(e)
	}
	tx, e := c.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var before, after int
	var raw []byte
	var out scheduler.CleanupOutcome
	stat := `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0) FROM pg_stat_xact_user_tables`
	if e = tx.QueryRow(ctx, stat).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, stat).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(raw, &out) != nil || out.RowsWritten != 99 || after-before != 99 {
		t.Fatal("full statement and Trigger write bound", out, after-before)
	}
	t.Log("observed complete transaction writes including all Trigger effects", after-before)
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,expires_at,emergency_stop)
 VALUES('excess-cleanup','controller','scheduler_budget_controller',uuidv7(),clock_timestamp()-interval '1 day',true)`); e != nil {
		t.Fatal(e)
	}
	_, e = c.Exec(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8)
	budgetSQLState(t, e, "54000")
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 8 {
		t.Fatal("bound refusal was not atomic")
	}
	retentionLedger(t, db)
}

func TestBudgetCleanupConcurrentReservationAndRollback(t *testing.T) {
	db, h := cleanupHistory(t, 9)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	barrier, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(ctx)
	if _, e = barrier.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	type reply struct {
		raw []byte
		err error
	}
	start := func() (int, <-chan reply) {
		conn, e := c.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		pid := int(conn.Conn().PgConn().PID())
		done := make(chan reply, 1)
		go func() {
			defer conn.Release()
			var raw []byte
			err := conn.QueryRow(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8).Scan(&raw)
			done <- reply{raw, err}
		}()
		return pid, done
	}
	p1, a := start()
	waitActivationBlock(t, db, p1)
	p2, b := start()
	waitActivationBlock(t, db, p2)
	var graph []byte
	if e = db.QueryRow(ctx, `SELECT jsonb_build_object('at',clock_timestamp(),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l WHERE pid=ANY($1)),
 'waits',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'xid',backend_xid,'began',xact_start,'query_start',query_start,'blockers',pg_blocking_pids(pid),'wait',wait_event)) FROM pg_stat_activity WHERE pid=ANY($1)))`, []int32{int32(p1), int32(p2)}).Scan(&graph); e != nil {
		t.Fatal(e)
	}
	if e = barrier.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log("cleanup parent/G wait and second same-scope connection", string(graph))
	x, y := <-a, <-b
	budgetSQLState(t, x.err, "00000")
	budgetSQLState(t, y.err, "00000")
	var first, second scheduler.CleanupOutcome
	if json.Unmarshal(x.raw, &first) != nil || json.Unmarshal(y.raw, &second) != nil || first.DeletedRuns+second.DeletedRuns != 9 {
		t.Fatal(string(x.raw), string(y.raw))
	}
	retentionLedger(t, db)
}

func TestBudgetCleanupMigrationLifecycle(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down", true)
	if queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname IN ('retention_candidates','cleanup_monitor','cleanup_target_key')`) != 0 {
		t.Fatal("Down kept capabilities")
	}
	bad := t.TempDir()
	files, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".sql") {
			data, e := os.ReadFile(filepath.Join(dir, f.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if strings.HasPrefix(f.Name(), "00010_") {
				data = []byte(strings.Replace(string(data), "CREATE FUNCTION scheduler_private.cleanup_monitor", "SELECT 1/0;\nCREATE FUNCTION scheduler_private.cleanup_monitor", 1))
			}
			if e = os.WriteFile(filepath.Join(bad, f.Name()), data, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	failed := goose(t, bad, "up", false)
	t.Log("original injected DDL failure", string(failed))
	if !strings.Contains(string(failed), "division by zero") || queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname IN ('retention_candidates','cleanup_monitor','cleanup_target_key')`) != 0 || queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 9 {
		t.Fatal("partial migration/capability survived")
	}
	goose(t, dir, "up", true)
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 10 {
		t.Fatal("not schema10")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname IN ('retention_candidates','cleanup_monitor','cleanup_target_key') AND prosecdef AND pg_get_userbyid(proowner)='devpilot_scheduler_owner'`) != 3 {
		t.Fatal("function boundary")
	}
}

func TestBudgetCleanupAtomicProjectionClearing(t *testing.T) {
	db, h := cleanupHistory(t, 9)
	c, ci := budgetController(t, db)
	ctx := context.Background()
	// These are expiry-audit FK references, not invented completed observations.
	// The second pointer targets the row OUTSIDE this batch, proving it is cleared
	// when the latest attempt retires rather than falling back to an older fact.
	if _, e := db.Exec(ctx, `UPDATE monitor_schedules SET
 last_attempt_run_id=(SELECT run_id FROM monitor_runs ORDER BY finished_at,run_id LIMIT 1),
 latest_result_run_id=(SELECT run_id FROM monitor_runs ORDER BY finished_at DESC,run_id DESC LIMIT 1)`); e != nil {
		t.Fatal(e)
	}
	tx, e := c.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var raw []byte
	if e = tx.QueryRow(ctx, cleanupSQL, ci, h.w, h.p, h.m, nil, nil, 8).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var oldPair bool
	if e = db.QueryRow(ctx, `SELECT last_attempt_run_id IS NOT NULL AND latest_result_run_id IS NOT NULL FROM monitor_schedules WHERE monitor_id=$1`, h.m).Scan(&oldPair); e != nil || !oldPair {
		t.Fatal("uncommitted cleanup exposed partial projection", e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND last_attempt_run_id IS NULL AND latest_result_run_id IS NULL AND last_fencing_token=9`, h.m) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 1 {
		t.Fatal("committed cleanup left reference or reset fence")
	}
	retentionLedger(t, db)
}

// Import real counted, never-prepared expiry rows at their original cold times.
// Administrative schema8 counters are derived from actual rows, never set to a
// fictional cap; M9 revalidates all of them. No physical receipt is fabricated.
func cleanupCapacitySeed(t *testing.T, level string) (*pgxpool.Pool, schedulerClaimHint, memberFixture) {
	t.Helper()
	db, _ := cleanupHistory(t, 0)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 8", true)
	nw, nm := 1, 100
	if level == "global" {
		nw = 10
	}
	if level == "monitor" {
		nm = 1
	}
	var first schedulerClaimHint
	var owner memberFixture
	for w := 0; w < nw; w++ {
		f, _, base := retentionFixture(t, db, fmt.Sprintf("archive-owner-%d", w))
		if _, e := db.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,last_refill_at,keep_until)
 VALUES('workspace',$1,2000000,clock_timestamp()-interval '1 day',clock_timestamp()-interval '23 hours'),
 ('project',$1||'/'||$2,1000000,clock_timestamp()-interval '1 day',clock_timestamp()-interval '23 hours')`, base.w, base.p); e != nil {
			t.Fatal(e)
		}
		for m := 0; m < nm; m++ {
			h := retentionClone(t, db, base)
			if _, e := db.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,last_refill_at,keep_until)
 VALUES('monitor',$1,1000000,clock_timestamp()-interval '1 day',clock_timestamp()-interval '23 hours')`, h.w+"/"+h.p+"/"+h.m); e != nil {
				t.Fatal(e)
			}
			retentionHistory(t, db, h, 100)
			if w == 0 && m == 0 {
				first = h
				owner = f
			}
		}
	}
	if _, e := db.Exec(ctx, `INSERT INTO scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state,release_basis,released_at)
 SELECT workspace_id,project_id,monitor_id,run_id,'released','never_prepared',physical_release_confirmed_at FROM monitor_runs;
 UPDATE scheduler_budget_state SET terminal_count=(SELECT count(*) FROM monitor_runs) WHERE kind='global';
 UPDATE scheduler_budget_state b SET terminal_count=x.n FROM (SELECT workspace_id::text k,count(*) n FROM monitor_runs GROUP BY workspace_id)x WHERE b.kind='workspace' AND b.budget_key=x.k;
 UPDATE scheduler_budget_state b SET terminal_count=x.n FROM (SELECT workspace_id::text||'/'||project_id::text||'/'||monitor_id::text k,count(*) n FROM monitor_runs GROUP BY workspace_id,project_id,monitor_id)x WHERE b.kind='monitor' AND b.budget_key=x.k`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "up", true)
	retentionLedger(t, db)
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	return db, first, owner
}

func cleanupLastSlot(t *testing.T, level string) {
	t.Helper()
	db, old, f := cleanupCapacitySeed(t, level)
	ctx := context.Background()
	c, ci := budgetController(t, db)
	var a, b schedulerClaimHint
	if level == "global" {
		_, _, a = retentionFixture(t, db, "new-a")
		_, _, b = retentionFixture(t, db, "new-b")
	} else {
		a = budgetExtra(t, db, f, old.p, "cleanup-next-a")
		b = budgetExtra(t, db, f, old.p, "cleanup-next-b")
	}
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	_, e := schedulerClaim(t, w1, i1, a)
	budgetSQLState(t, e, "P0001")
	if !strings.Contains(e.Error(), "retention_capacity") {
		t.Fatal(e)
	}
	barrier, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(ctx)
	if _, e = barrier.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	conn, e := c.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	pid := int(conn.Conn().PgConn().PID())
	cleaned := make(chan error, 1)
	go func() { _, err := conn.Exec(ctx, cleanupSQL, ci, old.w, old.p, old.m, nil, nil, 1); cleaned <- err }()
	waitActivationBlock(t, db, pid)
	p1, r1 := startSchedulerClaim(t, w1, i1, a)
	if level == "global" {
		waitActivationBlock(t, db, p1)
	}
	p2, r2 := startSchedulerClaim(t, w2, i2, b)
	if level == "global" {
		waitActivationBlock(t, db, p2)
	}
	if level == "workspace" {
		// Claim must SKIP this held Workspace; do not manufacture a wait or retry
		// the transaction. A new bounded round starts only after cleanup commits.
		for _, err := range []error{(<-r1).err, (<-r2).err} {
			budgetSQLState(t, err, "P0001")
			if !strings.Contains(err.Error(), "candidate_busy") {
				t.Fatal("Claim did not skip locked Workspace", err)
			}
		}
	}
	var graph []byte
	if e = db.QueryRow(ctx, `SELECT jsonb_build_object('at',clock_timestamp(),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l WHERE pid=ANY($1)),
 'waits',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'xid',backend_xid,'began',xact_start,'query_start',query_start,'blockers',pg_blocking_pids(pid),'wait',wait_event)) FROM pg_stat_activity WHERE pid=ANY($1)))`, []int32{int32(pid), int32(p1), int32(p2)}).Scan(&graph); e != nil {
		t.Fatal(e)
	}
	if e = barrier.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log("cleanup/Claim final capacity timeline", level, string(graph))
	budgetSQLState(t, <-cleaned, "00000")
	if level == "workspace" {
		_, r1 = startSchedulerClaim(t, w1, i1, a)
		_, r2 = startSchedulerClaim(t, w2, i2, b)
	}
	errors := []error{(<-r1).err, (<-r2).err}
	success := 0
	for _, err := range errors {
		if err == nil {
			success++
		} else {
			budgetSQLState(t, err, "P0001")
			if !strings.Contains(err.Error(), "retention_capacity") && !(level == "workspace" && strings.Contains(err.Error(), "candidate_busy")) {
				t.Fatal(err)
			}
		}
	}
	if success != 1 {
		t.Fatal("reclaimed slot was not single-use", errors)
	}
	if level == "workspace" {
		var refused error
		if errors[0] != nil {
			_, refused = schedulerClaim(t, w1, i1, a)
		} else {
			_, refused = schedulerClaim(t, w2, i2, b)
		}
		budgetSQLState(t, refused, "P0001")
		if !strings.Contains(refused.Error(), "retention_capacity") {
			t.Fatal("final Workspace cap not enforced", refused)
		}
	}
	want := 10000
	if level == "global" {
		want = 100000
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != want || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 || queryInt(t, db, `SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'`) != 1 {
		t.Fatal("retention/physical/pending drift")
	}
	retentionLedger(t, db)
}
func TestBudgetCleanupGlobalLastSlot(t *testing.T)    { cleanupLastSlot(t, "global") }
func TestBudgetCleanupWorkspaceLastSlot(t *testing.T) { cleanupLastSlot(t, "workspace") }
func TestBudgetCleanupMonitorLastSlot(t *testing.T) {
	db, h, f := cleanupCapacitySeed(t, "monitor")
	c, ci := budgetController(t, db)
	out := cleanupCall(t, c, ci, h, 1)
	if out.DeletedRuns != 1 {
		t.Fatal(out)
	}
	m, e := (monitor.Store{Pool: db}).Detail(context.Background(), f.od, h.w, h.p, h.m)
	if e != nil {
		t.Fatal(e)
	}
	fresh := budgetHint(t, db, f, m)
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	_, one := startSchedulerClaim(t, w1, i1, fresh)
	_, two := startSchedulerClaim(t, w2, i2, fresh)
	x, y := (<-one).err, (<-two).err
	if (x == nil) == (y == nil) {
		t.Fatal("two Claims used Monitor's last retained slot", x, y)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != 100 || queryInt(t, db, `SELECT last_fencing_token FROM monitor_schedules WHERE monitor_id=$1`, h.m) != 101 {
		t.Fatal("Monitor cap/fence drift")
	}
	retentionLedger(t, db)
}
