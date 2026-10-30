//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"

	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

// These tests intentionally cannot reuse the historical down/fixture cleanup.
// Only checkpoint3-pg.py creates this uniquely named, guarded disposable cluster.
func budgetDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := openDB(t)
	var cluster string
	if err := db.QueryRow(context.Background(), `SHOW cluster_name`).Scan(&cluster); err != nil || cluster != "devpilot-budget-disposable" {
		t.Fatal("requires isolated budget disposable PG18 cluster", err)
	}
	goose(t, filepath.Join(root(), "migrations"), "up", true)
	if _, err := db.Exec(context.Background(), `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(context.Background(), `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); err != nil {
			t.Error(err)
		}
		var raw []byte
		ledger := `SELECT jsonb_build_object('event','held_budget_ledger','at',clock_timestamp(),'budgets',(SELECT jsonb_agg(to_jsonb(b)) FROM scheduler_budget_state b),'runs',(SELECT jsonb_agg(to_jsonb(r)) FROM monitor_runs r),'reservations',(SELECT jsonb_agg(to_jsonb(q)) FROM scheduler_reservations q))`
		if strings.HasPrefix(t.Name(), "TestBudgetRetention") {
			// Large real row-boundary fixtures must not build an unbounded JSON aggregate.
			ledger = `SELECT jsonb_build_object('event','retention_ledger','at',clock_timestamp(),'runs',(SELECT count(*) FROM monitor_runs),'pending',(SELECT retention_pending_count FROM scheduler_budget_state WHERE kind='global'),'retained',(SELECT terminal_count FROM scheduler_budget_state WHERE kind='global'),'reservations',(SELECT count(*) FROM scheduler_reservations))`
		}
		if err := db.QueryRow(context.Background(), ledger).Scan(&raw); err != nil {
			t.Error(err)
		} else {
			t.Log(string(raw))
		}
		if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE physical_release_confirmed_at IS NULL`) > 0 {
			goose(t, filepath.Join(root(), "migrations"), "down", false)
			if queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=8 AND is_applied`) != 1 {
				t.Error("unsafe down destroyed ledger")
			}
		}
	})
	return db
}
func budgetSQLState(t *testing.T, err error, want string) {
	t.Helper()
	got := "00000"
	var p *pgconn.PgError
	if err != nil {
		got = "non-PG"
		if errors.As(err, &p) {
			got = p.Code
		}
	}
	t.Logf("SQLSTATE=%s error=%v", got, err)
	if got != want {
		t.Fatalf("wanted %s got %s: %v", want, got, err)
	}
}
func budgetHint(t *testing.T, db *pgxpool.Pool, f memberFixture, m monitor.Public) schedulerClaimHint {
	t.Helper()
	a := activationRead(t, db, f.od, m)
	if _, err := (monitor.Store{Pool: db}).ExecutionAuthorize(context.Background(), f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: m.Version, ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 3600}, monitor.ActivationETag(a)); err != nil {
		t.Fatal(err)
	}
	var h schedulerClaimHint
	if err := db.QueryRow(context.Background(), `SELECT s.workspace_id::text,s.project_id::text,s.monitor_id::text,b.user_id::text,b.membership_id::text,s.authorized_configuration_version::text,s.authorized_activation_generation::text,s.scheduler_epoch::text,s.next_run_id::text FROM monitor_schedules s JOIN workspace_members b ON b.workspace_id=s.workspace_id AND b.user_id=$2 WHERE s.monitor_id=$1`, m.ID, f.owner.ID).Scan(&h.w, &h.p, &h.m, &h.u, &h.b, &h.v, &h.g, &h.e, &h.r); err != nil {
		t.Fatal(err)
	}
	return h
}
func budgetExtra(t *testing.T, db *pgxpool.Pool, f memberFixture, pid, name string) schedulerClaimHint {
	t.Helper()
	m, err := (monitor.Store{Pool: db}).Create(context.Background(), f.od, f.w.ID, pid, monitorInput(name))
	if err != nil {
		t.Fatal(err)
	}
	return budgetHint(t, db, f, m)
}
func budgetPrepare(t *testing.T, db, supervisor *pgxpool.Pool, si, inc string, h schedulerClaimHint) string {
	t.Helper()
	var ns string
	if err := db.QueryRow(context.Background(), `SELECT uuidv7()::text`).Scan(&ns); err != nil {
		t.Fatal(err)
	}
	_, err := supervisor.Exec(context.Background(), `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,decode(repeat('ab',32),'hex'))`, si, h.w, h.p, h.m, h.r, inc, ns)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}
func budgetConsume(t *testing.T, s *pgxpool.Pool, si, ns string, h schedulerClaimHint) {
	t.Helper()
	_, err := s.Exec(context.Background(), `SELECT scheduler_private.consume_input($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,1)`, si, h.w, h.p, h.m, h.r, ns)
	if err != nil {
		t.Fatal(err)
	}
}
func budgetController(t *testing.T, db *pgxpool.Pool) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE ROLE scheduler_budget_controller LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_controller; ALTER ROLE scheduler_budget_controller SET statement_timeout='2s'; ALTER ROLE scheduler_budget_controller SET transaction_timeout='2s'; ALTER ROLE scheduler_budget_controller SET lock_timeout='250ms'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM scheduler_control WHERE login_name='scheduler_budget_controller'; DROP OWNED BY scheduler_budget_controller; DROP ROLE scheduler_budget_controller`); err != nil {
			t.Error(err)
		}
	})
	var inc string
	if err := db.QueryRow(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,verified_at,expires_at,emergency_stop) VALUES('budget-controller','controller','scheduler_budget_controller',uuidv7(),clock_timestamp(),clock_timestamp()+interval '1 hour',false) RETURNING incarnation::text`).Scan(&inc); err != nil {
		t.Fatal(err)
	}
	cfg := db.Config()
	cfg.ConnConfig.User = "scheduler_budget_controller"
	cfg.ConnConfig.Password = "disposable_fixture_only"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, inc
}
func budgetFixtures(t *testing.T, db *pgxpool.Pool, n int) []schedulerClaimHint {
	var hs []schedulerClaimHint
	for i := range n {
		t.Run(fmt.Sprintf("workspace-%02d", i), func(t *testing.T) { _, _, h := schedulerClaimFixtureSeconds(t, db, 3600); hs = append(hs, h) })
	}
	return hs
}
func TestBudgetGlobalLastSlot(t *testing.T) {
	db := budgetDB(t)
	hs := budgetFixtures(t, db, 33)
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	for _, h := range hs[:31] {
		_, err := schedulerClaim(t, w1, i1, h)
		budgetSQLState(t, err, "00000")
	}
	barrier, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(context.Background())
	if _, err = barrier.Exec(context.Background(), `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	p1, c1 := startSchedulerClaim(t, w1, i1, hs[31])
	waitActivationBlock(t, db, p1)
	p2, c2 := startSchedulerClaim(t, w2, i2, hs[32])
	waitActivationBlock(t, db, p2)
	schedulerLockReceipt(t, db, "global31-two-workers-last-slot", p1, p2)
	if err = barrier.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	r1, r2 := <-c1, <-c2
	budgetSQLState(t, r1.err, "00000")
	budgetSQLState(t, r2.err, "P0001")
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 32 || queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 32 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations`) != 32 {
		t.Fatal("non-atomic global32 admission")
	}
	if _, err = schedulerClaim(t, w2, i2, hs[32]); err == nil {
		t.Fatal("exceeded32")
	}
	budgetSQLState(t, err, "P0001")
	// Cancellation of an uncommitted Claim must roll back all counters and rows.
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, hs[32].m) != 0 {
		t.Fatal("failed claim leaked row")
	}
}
func TestBudgetScopedSlots(t *testing.T) {
	db := budgetDB(t)
	f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	h2 := budgetExtra(t, db, f, m.ProjectID, "m2")
	h3 := budgetExtra(t, db, f, m.ProjectID, "m3")
	for _, next := range []schedulerClaimHint{h, h2} {
		_, err := schedulerClaim(t, w, i, next)
		budgetSQLState(t, err, "00000")
	}
	_, err := schedulerClaim(t, w2, i2, h3)
	budgetSQLState(t, err, "P0001")
	_, err = schedulerClaim(t, w2, i2, h)
	budgetSQLState(t, err, "P0001")
	for j := range 2 {
		name := fmt.Sprintf("project%d", j)
		p, err := (project.Store{Pool: db}).Create(context.Background(), f.od, f.w.ID, project.Input{Name: &name})
		if err != nil {
			t.Fatal(err)
		}
		extra := budgetExtra(t, db, f, p.ID, "extra")
		_, err = schedulerClaim(t, w, i, extra)
		budgetSQLState(t, err, "00000")
	}
	name := "overflow"
	p, err := (project.Store{Pool: db}).Create(context.Background(), f.od, f.w.ID, project.Input{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	over := budgetExtra(t, db, f, p.ID, "over")
	_, err = schedulerClaim(t, w2, i2, over)
	budgetSQLState(t, err, "P0001")
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='workspace' AND budget_key=$1`, h.w) != 4 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p) != 2 || queryInt(t, db, `SELECT max(in_use) FROM scheduler_budget_state WHERE kind='monitor'`) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 4 {
		t.Fatal("scoped4/2/1 or rollback violated")
	}
}
func TestBudgetStartRateRollback(t *testing.T) {
	db := budgetDB(t)
	f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
	h2 := budgetExtra(t, db, f, m.ProjectID, "second")
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	for _, x := range []schedulerClaimHint{h, h2} {
		_, err := schedulerClaim(t, w, i, x)
		if err != nil {
			t.Fatal(err)
		}
		budgetPrepare(t, db, s, si, i, x)
	}
	if _, err := schedulerStart(t, w, i, h); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := db.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, err := schedulerStart(t, w, i, h2)
	budgetSQLState(t, err, "P0001")
	var after []byte
	if err := db.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed Start refunded/consumed tokens or changed cooling")
	}
	// Real database time, not a fake clock: the project refill period is two seconds.
	var delay float64
	if err := db.QueryRow(context.Background(), `SELECT GREATEST(0,extract(epoch FROM last_refill_at+interval '2 seconds'-clock_timestamp()))::float8 FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p).Scan(&delay); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Duration(delay*float64(time.Second)) + time.Millisecond)
	if _, err = schedulerStart(t, w, i, h2); err != nil {
		t.Fatal(err)
	}
	// Permission/configuration revocation cannot refund an accepted Start.
	var tokens int64
	if err := db.QueryRow(context.Background(), `SELECT tokens FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	newName := "revoked"
	if _, err := (monitor.Store{Pool: db}).Update(context.Background(), f.od, h.w, h.p, h.m, monitor.Input{Name: &newName}, monitor.ETag(m)); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT tokens FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p) != int(tokens) {
		t.Fatal("PATCH refunded Start tokens")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE physical_release_confirmed_at IS NOT NULL`) != 0 {
		t.Fatal("test forged physical release")
	}
}
func TestBudgetArithmeticAndKeyCapacity(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	// Private helper arithmetic executed under the nonLOGIN function owner in a
	// disposable transaction. Runtime identities never acquire this capability.
	for _, c := range []struct {
		kind      string
		cap, rate int64
		period    time.Duration
	}{{"global", 8000000, 8000000, time.Second / 8}, {"workspace", 2000000, 1000000, time.Second}, {"project", 1000000, 500000, 2 * time.Second}, {"origin", 1000000, 0, 60 * time.Second}, {"ip", 1000000, 0, time.Second}} {
		t.Run(c.kind, func(t *testing.T) {
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			key := strings.Repeat("a", 64)
			if c.kind == "global" {
				key = "global"
			} else if c.kind == "workspace" {
				key = "00000000-0000-0000-0000-000000000001"
			} else if c.kind == "project" {
				key = "00000000-0000-0000-0000-000000000001/00000000-0000-0000-0000-000000000002"
			}
			var n time.Time
			if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if c.kind == "global" {
				_, err = tx.Exec(ctx, `UPDATE scheduler_budget_state SET tokens=0,last_refill_at=$1 WHERE kind='global'`, n)
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,last_refill_at,keep_until) VALUES($1,$2,0,$3::timestamptz,$3::timestamptz+interval '1 hour')`, c.kind, key, n)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `SET LOCAL ROLE devpilot_scheduler_owner`); err != nil {
				t.Fatal(err)
			}
			invoke := func(at time.Time) error {
				sp, err := tx.Begin(ctx)
				if err != nil {
					return err
				}
				defer sp.Rollback(ctx)
				sql := `SELECT scheduler_private.consume_token($1,$2,$3,$4,$5)`
				args := []any{c.kind, key, c.cap, c.rate, at}
				if c.rate == 0 {
					sql = `SELECT scheduler_private.consume_target_token($1,$2,$3)`
					args = []any{c.kind, key, at}
				}
				_, err = sp.Exec(ctx, sql, args...)
				if err == nil {
					err = sp.Commit(ctx)
				}
				return err
			}
			budgetSQLState(t, invoke(n.Add(-time.Microsecond)), "P0001")
			budgetSQLState(t, invoke(n.Add(c.period-time.Microsecond)), "P0001")
			budgetSQLState(t, invoke(n.Add(c.period)), "00000")
		})
	}
	// Real10,000-key boundary, not a forged global counter.
	if _, err := db.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,keep_until) SELECT 'origin',lpad(to_hex(i),64,'0'),1000000,clock_timestamp()+interval '1 hour' FROM generate_series(1,9999) i`); err != nil {
		t.Fatal(err)
	}
	_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	_, err := (scheduler.Store{DB: w, Incarnation: i}).Claim(ctx, checkpointTwoHint(h))
	if !errors.Is(err, scheduler.ErrCapacity) {
		t.Fatal("capacity not fixed refusal", err)
	}
	if queryInt(t, db, `SELECT count(*) FROM scheduler_budget_state`) != 10000 || queryInt(t, db, `SELECT key_count FROM scheduler_budget_state WHERE kind='global'`) != 10000 || queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
		t.Fatal("key cap rollback violated")
	}
}

func budgetAsync(t *testing.T, p *pgxpool.Pool, sql string, args ...any) (int, <-chan error) {
	t.Helper()
	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pid := int(c.Conn().PgConn().PID())
	result := make(chan error, 1)
	go func() {
		defer c.Release()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		tx, err := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err == nil {
			var ignored []byte
			err = tx.QueryRow(ctx, sql, args...).Scan(&ignored)
			if err == nil {
				err = tx.Commit(ctx)
			}
			_ = tx.Rollback(context.Background())
		}
		result <- err
	}()
	return pid, result
}
func budgetWidePool(t *testing.T, p *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg := p.Config()
	cfg.MaxConns = 16
	wide, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wide.Close)
	return wide
}

// This one rate-burst fixture observes server transaction admission. Each
// independent connection still commits a real READ COMMITTED transaction;
// autocommit removes a client scheduling pause between function return and
// Commit from the125ms burst prerequisite. Production Start/Commit and its ACK
// tests remain explicit and unchanged. No Probe is launched by this fixture.
func budgetBurstAutocommit(t *testing.T, p *pgxpool.Pool, sql string, args ...any) (int, <-chan error) {
	t.Helper()
	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pid := int(c.Conn().PgConn().PID())
	result := make(chan error, 1)
	go func() {
		err := func() error {
			defer c.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var ignored []byte
			return c.QueryRow(ctx, sql, args...).Scan(&ignored)
		}()
		result <- err
	}()
	return pid, result
}
func TestBudgetGlobalRateConcurrentStarts(t *testing.T) {
	db := budgetDB(t)
	hs := budgetFixtures(t, db, 9)
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	w1 = budgetWidePool(t, w1)
	w2 = budgetWidePool(t, w2)
	s, si := schedulerSupervisorLogin(t, db)
	for j, h := range hs {
		w, inc := w1, i1
		if j%2 == 1 {
			w, inc = w2, i2
		}
		if _, err := schedulerClaim(t, w, inc, h); err != nil {
			t.Fatal(err)
		}
		budgetPrepare(t, db, s, si, inc, h)
	}
	barrier, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(context.Background())
	if _, err = barrier.Exec(context.Background(), `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	var cs []<-chan error
	var pids []int
	for j, h := range hs {
		w, inc := w1, i1
		if j%2 == 1 {
			w, inc = w2, i2
		}
		pid, ch := budgetBurstAutocommit(t, w, `SELECT scheduler_private.start_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,1)`, inc, h.w, h.p, h.m, h.u, h.b, h.r)
		waitActivationBlock(t, db, pid)
		cs = append(cs, ch)
		pids = append(pids, pid)
	}
	locks := schedulerLockSnapshot(t, db, "nine-starts-global-burst8", pids...)
	if err = barrier.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Log(string(locks))
	passed, refused := 0, 0
	for _, ch := range cs {
		err := <-ch
		if err == nil {
			passed++
		} else {
			budgetSQLState(t, err, "P0001")
			refused++
		}
	}
	if passed != 8 || refused != 1 {
		t.Fatalf("global8 burst: passed%d refused%d", passed, refused)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE attempt_state='started'`) != 8 {
		t.Fatal("uncommitted rate admitted")
	}
	var span float64
	if err := db.QueryRow(context.Background(), `SELECT extract(epoch FROM max(started_at)-min(started_at))::float8 FROM monitor_runs WHERE attempt_state='started'`).Scan(&span); err != nil || span >= .125 {
		t.Fatal("test did not witness same burst window", span, err)
	}
	t.Logf("real DB nine Start transactions:8 accepted,1 refused, accepted span=%fs", span)
}
func TestBudgetWorkspaceRateAcrossProjects(t *testing.T) {
	db := budgetDB(t)
	f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
	hs := []schedulerClaimHint{h}
	for j := range 2 {
		name := fmt.Sprintf("p%d", j)
		p, err := (project.Store{Pool: db}).Create(context.Background(), f.od, m.WorkspaceID, project.Input{Name: &name})
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, budgetExtra(t, db, f, p.ID, "target"))
	}
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	for _, h := range hs {
		if _, err := schedulerClaim(t, w, i, h); err != nil {
			t.Fatal(err)
		}
		budgetPrepare(t, db, s, si, i, h)
	}
	for _, h := range hs[:2] {
		if _, err := schedulerStart(t, w, i, h); err != nil {
			t.Fatal(err)
		}
	}
	_, err := schedulerStart(t, w, i, hs[2])
	budgetSQLState(t, err, "P0001")
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE attempt_state='started'`) != 2 {
		t.Fatal("workspace burst2 exceeded")
	}
	time.Sleep(time.Second + time.Millisecond)
	if _, err = schedulerStart(t, w, i, hs[2]); err != nil {
		t.Fatal(err)
	}
}
func TestBudgetMonitorCoolingAcrossVersionAndRestart(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	interval := int64(300)
	var err error
	m, err = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{IntervalSeconds: &interval}, monitor.ETag(m))
	if err != nil {
		t.Fatal(err)
	}
	h := budgetHint(t, db, f, m)
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	if _, err = schedulerClaim(t, w, i, h); err != nil {
		t.Fatal(err)
	}
	budgetPrepare(t, db, s, si, i, h)
	// Historical cooling fixture, with no fabricated Run completion or release.
	if _, err = db.Exec(ctx, `UPDATE monitor_schedules SET last_started_at=clock_timestamp()-interval '100 seconds',next_start_not_before=clock_timestamp()-interval '40 seconds' WHERE monitor_id=$1`, h.m); err != nil {
		t.Fatal(err)
	}
	_, err = schedulerStart(t, w, i, h)
	budgetSQLState(t, err, "P0001") // lastStart+CURRENT300, although old60 cooldown elapsed
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE attempt_state='started'`) != 0 {
		t.Fatal("current interval gate bypass")
	}
	// A different Monitor has durable cooling before any pending execution. A
	// valid PATCH/re-authorization and new Worker connection cannot shorten it.
	m2, err := (monitor.Store{Pool: db}).Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("cooling"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, m2.WorkspaceID, m2.ProjectID, m2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE monitor_schedules SET last_started_at=clock_timestamp()-interval '30 seconds',next_start_not_before=clock_timestamp()+interval '600 seconds' WHERE monitor_id=$1`, m2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO scheduler_budget_state(kind,budget_key,tokens,not_before,keep_until) VALUES('monitor',$1,1000000,clock_timestamp()+interval '700 seconds',clock_timestamp()+interval '1 hour')`, m2.WorkspaceID+"/"+m2.ProjectID+"/"+m2.ID); err != nil {
		t.Fatal(err)
	}
	var old time.Time
	if err = db.QueryRow(ctx, `SELECT next_start_not_before FROM monitor_schedules WHERE monitor_id=$1`, m2.ID).Scan(&old); err != nil {
		t.Fatal(err)
	}
	name := "new configuration"
	m2, err = (monitor.Store{Pool: db}).Update(ctx, f.od, m2.WorkspaceID, m2.ProjectID, m2.ID, monitor.Input{Name: &name}, monitor.ETag(m2))
	if err != nil {
		t.Fatal(err)
	}
	h2 := budgetHint(t, db, f, m2)
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	_, err = schedulerClaim(t, w2, i2, h2)
	budgetSQLState(t, err, "P0001")
	var after time.Time
	if err = db.QueryRow(ctx, `SELECT next_start_not_before FROM monitor_schedules WHERE monitor_id=$1`, h2.m).Scan(&after); err != nil || !old.Equal(after) {
		t.Fatal("version/g/restart shortened cooling", err)
	}
	hints, err := (scheduler.Store{DB: w2, Incarnation: i2}).Candidates(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range hints {
		if x.MonitorID == h2.m {
			t.Fatal("scan ignored cooling")
		}
	}
}

func TestBudgetScopedLastSlotInterleavings(t *testing.T) {
	db := budgetDB(t)
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	for _, scope := range []string{"workspace", "project", "monitor"} {
		t.Run(scope, func(t *testing.T) {
			f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
			first, second := h, h
			var prefill []schedulerClaimHint
			if scope == "project" {
				prefill = []schedulerClaimHint{h}
				first = budgetExtra(t, db, f, m.ProjectID, "last")
				second = budgetExtra(t, db, f, m.ProjectID, "overflow")
			}
			if scope == "workspace" {
				prefill = []schedulerClaimHint{h, budgetExtra(t, db, f, m.ProjectID, "p1second")}
				var projs []string
				for j := range 2 {
					name := fmt.Sprintf("other%d", j)
					p, err := (project.Store{Pool: db}).Create(context.Background(), f.od, m.WorkspaceID, project.Input{Name: &name})
					if err != nil {
						t.Fatal(err)
					}
					projs = append(projs, p.ID)
				}
				prefill = append(prefill, budgetExtra(t, db, f, projs[0], "p2first"))
				first = budgetExtra(t, db, f, projs[0], "last")
				second = budgetExtra(t, db, f, projs[1], "overflow")
			}
			second = checkpointTwoPromote(t, db, f, second)
			for _, x := range prefill {
				_, err := schedulerClaim(t, w1, i1, x)
				budgetSQLState(t, err, "00000")
			}
			barrier, err := db.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer barrier.Rollback(context.Background())
			if _, err = barrier.Exec(context.Background(), `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			pid, ch := startSchedulerClaim(t, w1, i1, first)
			waitActivationBlock(t, db, pid)
			_, err = schedulerClaim(t, w2, i2, second)
			budgetSQLState(t, err, "P0001")
			checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, second.u)
			schedulerLockReceipt(t, db, "last-"+scope+"-different-owner-workspace-skip", pid)
			if err = barrier.Commit(context.Background()); err != nil {
				t.Fatal(err)
			}
			budgetSQLState(t, (<-ch).err, "00000")
			_, err = schedulerClaim(t, w2, i2, second)
			budgetSQLState(t, err, "P0001")
			key := h.w
			limit := 4
			if scope == "project" {
				key += "/" + h.p
				limit = 2
			}
			if scope == "monitor" {
				key += "/" + h.p + "/" + h.m
				limit = 1
			}
			if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind=$1 AND budget_key=$2`, scope, key) != limit {
				t.Fatal("last scoped slot accounting", scope)
			}
		})
	}
}
