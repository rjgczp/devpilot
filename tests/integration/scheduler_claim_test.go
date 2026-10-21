//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
)

type schedulerClaimHint struct{ w, p, m, u, b, v, g, e, r string }

func (h schedulerClaimHint) args(inc string) []any {
	return []any{inc, h.w, h.p, h.m, h.u, h.b, h.v, h.g, h.e, h.r}
}

const schedulerClaimSQL = `SELECT scheduler_private.claim($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bigint,$8::bigint,$9::bigint,$10::uuid)`

func schedulerClaimFixture(t *testing.T, db *pgxpool.Pool) (memberFixture, monitor.Public, schedulerClaimHint) {
	t.Helper()
	return schedulerClaimFixtureSeconds(t, db, 60)
}

func schedulerClaimFixtureSeconds(t *testing.T, db *pgxpool.Pool, seconds int) (memberFixture, monitor.Public, schedulerClaimHint) {
	t.Helper()
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	a := activationRead(t, db, f.od, m)
	if _, err := (monitor.Store{Pool: db}).ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: seconds}, monitor.ActivationETag(a)); err != nil {
		t.Fatal(err)
	}
	var h schedulerClaimHint
	if err := db.QueryRow(ctx, `SELECT s.workspace_id::text,s.project_id::text,s.monitor_id::text,b.user_id::text,b.membership_id::text,s.authorized_configuration_version::text,s.authorized_activation_generation::text,s.scheduler_epoch::text,s.next_run_id::text FROM monitor_schedules s JOIN workspace_members b ON b.workspace_id=s.workspace_id AND b.user_id=$2 WHERE s.monitor_id=$1`, m.ID, f.owner.ID).Scan(&h.w, &h.p, &h.m, &h.u, &h.b, &h.v, &h.g, &h.e, &h.r); err != nil {
		t.Fatal(err)
	}
	return f, m, h
}

// Fixture bootstrap attests database-only cases; no namespace or Probe exists.
// This administrative gate is never an implementation of network permission.
func schedulerDatabaseOnlyGate(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(context.Background(), `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'; UPDATE monitor_runs SET attempt_state=CASE WHEN attempt_state='leased' THEN 'canceled' WHEN attempt_state='started' THEN 'unknown' ELSE attempt_state END,finished_at=COALESCE(finished_at,GREATEST(clock_timestamp(),claimed_at,started_at)),stop_reason=CASE WHEN attempt_state IN ('leased','started') THEN 'worker_lost' ELSE stop_reason END,physical_release_confirmed_at=GREATEST(clock_timestamp(),claimed_at,started_at) WHERE physical_release_confirmed_at IS NULL`); err != nil {
			t.Error(err)
		}
	})
}
func schedulerWorkerLogin(t *testing.T, db *pgxpool.Pool, name string) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	// Callers use only these fixed fixture identifiers, never arbitrary SQL input.
	if name != "scheduler_claim_worker1" && name != "scheduler_claim_worker2" {
		t.Fatal("invalid fixture identity")
	}
	sql := `CREATE ROLE scheduler_claim_worker1 LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_worker; ALTER ROLE scheduler_claim_worker1 SET statement_timeout='2s'; ALTER ROLE scheduler_claim_worker1 SET transaction_timeout='2s'; ALTER ROLE scheduler_claim_worker1 SET lock_timeout='250ms'`
	if name == "scheduler_claim_worker2" {
		sql = `CREATE ROLE scheduler_claim_worker2 LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_worker; ALTER ROLE scheduler_claim_worker2 SET statement_timeout='2s'; ALTER ROLE scheduler_claim_worker2 SET transaction_timeout='2s'; ALTER ROLE scheduler_claim_worker2 SET lock_timeout='250ms'`
	}
	if _, err := db.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM scheduler_control WHERE login_name=$1`, name); err != nil {
			t.Error(err)
		}
		if _, err := db.Exec(ctx, `DROP OWNED BY `+name+`;DROP ROLE `+name); err != nil {
			t.Error(err)
		}
	})
	var inc string
	if err := db.QueryRow(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,emergency_stop,verified_at,expires_at) VALUES($1::text,'worker',$1::name,uuidv7(),false,clock_timestamp(),clock_timestamp()+interval '1 hour') RETURNING incarnation::text`, name).Scan(&inc); err != nil {
		t.Fatal(err)
	}
	cfg := db.Config()
	cfg.ConnConfig.User = name
	cfg.ConnConfig.Password = "disposable_fixture_only"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, inc
}
func schedulerClaim(t *testing.T, pool *pgxpool.Pool, inc string, h schedulerClaimHint) (map[string]any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	var raw []byte
	if err = tx.QueryRow(ctx, schedulerClaimSQL, h.args(inc)...).Scan(&raw); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	var got map[string]any
	err = json.Unmarshal(raw, &got)
	return got, err
}
func TestSchedulerClaimAtomicFenceAndSlots(t *testing.T) {
	db := workspaceDB(t)
	_, m, h := schedulerClaimFixture(t, db)
	worker, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	got, err := schedulerClaim(t, worker, inc, h)
	if err != nil {
		t.Fatal(err)
	}
	if got["fencing_token"] != "1" || got["run_id"] != h.r {
		t.Fatal("wrong lineage", got)
	}
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global' AND budget_key='global'`) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND attempt_state='leased' AND started_at IS NULL`, m.ID) != 1 {
		t.Fatal("claim did not atomically reserve")
	}
	if _, err = schedulerClaim(t, worker, inc, h); err == nil {
		t.Fatal("same cycle claimed twice")
	}
	if queryInt(t, db, `SELECT last_fencing_token FROM monitor_schedules WHERE monitor_id=$1`, m.ID) != 1 {
		t.Fatal("failed claim consumed fence")
	}
	if _, err = worker.Exec(context.Background(), `SELECT * FROM public.monitor_runs`); err == nil {
		t.Fatal("worker received table access")
	}
	forged := inc[:len(inc)-1] + "f"
	if forged == inc {
		forged = inc[:len(inc)-1] + "0"
	}
	_, err = schedulerClaim(t, worker, forged, h)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
		t.Fatal("forged incarnation", err)
	}
}
func TestSchedulerClaimWorkspaceSkipReleasesUser(t *testing.T) {
	db := workspaceDB(t)
	f, _, h := schedulerClaimFixture(t, db)
	worker, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	ctx := context.Background()
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	_, err = schedulerClaim(t, worker, inc, h)
	if err == nil || time.Since(before) > 200*time.Millisecond {
		t.Fatal("workspace did not skip", err)
	}
	inspect, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inspect.Rollback(ctx)
	if _, err = inspect.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.owner.ID); err != nil {
		t.Fatal("skipped workspace retained user lock", err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
		t.Fatal("skip left provisional work")
	}
}
