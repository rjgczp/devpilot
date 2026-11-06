//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestCheckpointTwoAttemptTransitionMatrix(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	states := []string{"leased", "started", "completed", "canceled", "expired", "unknown"}
	for _, from := range states {
		for _, to := range states {
			t.Run(from+"-to-"+to, func(t *testing.T) {
				_, m, h := schedulerClaimFixture(t, db)
				var run string
				err := db.QueryRow(ctx, `WITH n AS(SELECT clock_timestamp() t) INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state,started_at,finished_at,result_commit_id,duration_ms,http_status,success)
  SELECT $1,$2,$3,uuidv7(),1,2,1,1,t,t,session_user,uuidv7(),t+interval '45 seconds',$4,
  CASE WHEN $4 IN ('started','completed','unknown') THEN t END,CASE WHEN $4 IN ('completed','canceled','expired','unknown') THEN t END,
  CASE WHEN $4='completed' THEN uuidv7() END,CASE WHEN $4='completed' THEN 1 END,CASE WHEN $4='completed' THEN 200 END,CASE WHEN $4='completed' THEN true END FROM n RETURNING run_id::text`, h.w, h.p, m.ID, from).Scan(&run)
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec(ctx, `UPDATE monitor_runs SET attempt_state=$2,started_at=CASE WHEN $2 IN ('started','completed','unknown') THEN COALESCE(started_at,clock_timestamp()) ELSE started_at END,
  finished_at=CASE WHEN $2 IN ('completed','canceled','expired','unknown') THEN COALESCE(finished_at,clock_timestamp()) ELSE NULL END,
  result_commit_id=CASE WHEN $2='completed' THEN COALESCE(result_commit_id,uuidv7()) END,duration_ms=CASE WHEN $2='completed' THEN 1 END,http_status=CASE WHEN $2='completed' THEN 200 END,success=CASE WHEN $2='completed' THEN true END WHERE run_id=$1`, run, to)
				allowed := from == to || (from == "leased" && (to == "started" || to == "canceled" || to == "expired")) || (from == "started" && (to == "completed" || to == "unknown"))
				if allowed && err != nil {
					t.Fatal("valid transition", err)
				}
				if !allowed {
					var p *pgconn.PgError
					if !errors.As(err, &p) || p.Code != "23514" {
						t.Fatalf("illegal transition accepted or wrong SQLSTATE: %v", err)
					}
				}
				if _, err = db.Exec(ctx, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, run); err != nil {
					t.Fatal(err)
				}
				t.Logf("transition=%s->%s allowed=%v", from, to, allowed)
			})
		}
	}
}
func TestCheckpointTwoFenceOverflowAndClockRegression(t *testing.T) {
	for _, kind := range []string{"fence-overflow", "epoch-overflow", "clock-back-small", "clock-back-large"} {
		t.Run(kind, func(t *testing.T) {
			db := schedulerHistoricalDB(t)
			ctx := context.Background()
			_, _, h := schedulerClaimFixture(t, db)
			w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
			schedulerDatabaseOnlyGate(t, db)
			switch kind {
			case "fence-overflow":
				if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET last_fencing_token=9223372036854775807 WHERE monitor_id=$1`, h.m); err != nil {
					t.Fatal(err)
				}
			case "epoch-overflow":
				if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET scheduler_epoch=9223372036854775807 WHERE monitor_id=$1`, h.m); err != nil {
					t.Fatal(err)
				}
				_, err := db.Exec(ctx, `UPDATE monitor_schedules SET scheduler_epoch=scheduler_epoch+1 WHERE monitor_id=$1`, h.m)
				var p *pgconn.PgError
				if !errors.As(err, &p) || p.Code != "22003" {
					t.Fatal(err)
				}
				return
			default:
				delta := "100 milliseconds"
				if kind == "clock-back-large" {
					delta = "1 hour"
				}
				if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET updated_at=clock_timestamp()+$2::interval WHERE monitor_id=$1`, h.m, delta); err != nil {
					t.Fatal(err)
				}
			}
			_, err := schedulerClaim(t, w, inc, h)
			if kind == "clock-back-small" {
				if err != nil {
					t.Fatal(err)
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs r JOIN monitor_schedules s USING(workspace_id,project_id,monitor_id) WHERE r.monitor_id=$1 AND r.claimed_at=s.updated_at AND r.lease_until=r.claimed_at+interval '45 seconds'`, h.m) != 1 {
					t.Fatal("durable clock regressed")
				}
				schedulerPreparedDatabaseFixture(t, db, h)
				if _, err = schedulerStart(t, w, inc, h); err != nil {
					t.Fatal("small backward step broke monotonic Start", err)
				}
			} else {
				var p *pgconn.PgError
				code := "P0001"
				if kind == "fence-overflow" {
					code = "22003"
				}
				if !errors.As(err, &p) || p.Code != code {
					t.Fatal(fmt.Sprintf("expected %s", code), err)
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != 0 {
					t.Fatal("overflow/clock refusal left run")
				}
				t.Logf("SQLSTATE=%s reason=%s", p.Code, p.Message)
			}
			checkpointTwoFinal(t, db, h)
		})
	}
}
