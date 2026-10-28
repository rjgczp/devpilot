//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func schedulerSupervisorLogin(t *testing.T, db *pgxpool.Pool) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE ROLE scheduler_claim_supervisor LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_supervisor; ALTER ROLE scheduler_claim_supervisor SET statement_timeout='2s'; ALTER ROLE scheduler_claim_supervisor SET transaction_timeout='2s'; ALTER ROLE scheduler_claim_supervisor SET lock_timeout='250ms'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM scheduler_control WHERE login_name='scheduler_claim_supervisor'; DROP OWNED BY scheduler_claim_supervisor; DROP ROLE scheduler_claim_supervisor`); err != nil {
			t.Error(err)
		}
	})
	var inc string
	if err := db.QueryRow(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,emergency_stop,verified_at,expires_at) VALUES('supervisor-fixture','supervisor','scheduler_claim_supervisor',uuidv7(),false,clock_timestamp(),clock_timestamp()+interval '1 hour') RETURNING incarnation::text`).Scan(&inc); err != nil {
		t.Fatal(err)
	}
	cfg := db.Config()
	cfg.ConnConfig.User = "scheduler_claim_supervisor"
	cfg.ConnConfig.Password = "disposable_fixture_only"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, inc
}
func TestSchedulerSupervisorOwnsOneInputCapability(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, _, h := schedulerClaimFixture(t, db)
	worker, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	supervisor, si := schedulerSupervisorLogin(t, db)
	schedulerDatabaseOnlyGate(t, db)
	if _, err := schedulerClaim(t, worker, inc, h); err != nil {
		t.Fatal(err)
	}
	const prepare = `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,decode(repeat('ab',32),'hex'))`
	var namespace string
	if err := db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&namespace); err != nil {
		t.Fatal(err)
	}
	args := []any{si, h.w, h.p, h.m, h.r, inc, namespace}
	if _, err := worker.Exec(ctx, prepare, args...); err == nil {
		t.Fatal("worker forged preparation")
	}
	// This validates restricted DB capability only; no Linux process or namespace
	// exists here. Physical acceptance must separately verify the Supervisor.
	if _, err := supervisor.Exec(ctx, prepare, args...); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Exec(ctx, prepare, args...); err == nil {
		t.Fatal("preparation capability replay")
	}
	if _, err := schedulerStart(t, worker, inc, h); err != nil {
		t.Fatal(err)
	}
	const consume = `SELECT scheduler_private.consume_input($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,1)`
	input := []any{si, h.w, h.p, h.m, h.r, namespace}
	if _, err := worker.Exec(ctx, consume, input...); err == nil {
		t.Fatal("worker consumed supervisor-only input")
	}
	if _, err := supervisor.Exec(ctx, consume, input...); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Exec(ctx, consume, input...); err == nil {
		t.Fatal("input capability replay")
	}
	if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE monitor_id=$1 AND input_consumed_at IS NOT NULL`, h.m) != 1 {
		t.Fatal("missing durable consumption")
	}
	if _, err := db.Exec(ctx, `UPDATE scheduler_reservations SET input_consumed_at=NULL WHERE monitor_id=$1`, h.m); err == nil {
		t.Fatal("even administrator SQL silently restored consumed input")
	}
	for _, sql := range []string{`SELECT password_hash FROM users`, `SELECT token_hash FROM sessions`, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp()`, `UPDATE scheduler_control SET gate='fixture'`, `SET ROLE devpilot_scheduler_owner`, `CREATE TEMP TABLE forged(i integer)`} {
		if _, err := supervisor.Exec(ctx, sql); err == nil {
			t.Fatal("supervisor escaped narrow functions", sql)
		}
	}
}
