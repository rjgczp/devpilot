//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
)

const schedulerStartSQL = `SELECT scheduler_private.start_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,$8::bigint)`

func schedulerStart(t *testing.T, pool *pgxpool.Pool, inc string, h schedulerClaimHint) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	var raw []byte
	err = tx.QueryRow(ctx, schedulerStartSQL, inc, h.w, h.p, h.m, h.u, h.b, h.r, "1").Scan(&raw)
	if err != nil {
		return nil, err
	}
	err = tx.Commit(ctx)
	return raw, err
}

// Administrative projection fixture, NEVER a real physical-preparation receipt.
// Real Supervisor acceptance must produce these rows through its own capability.
func schedulerPreparedDatabaseFixture(t *testing.T, db *pgxpool.Pool, h schedulerClaimHint) {
	t.Helper()
	ctx := context.Background()
	var supervisor string
	if err := db.QueryRow(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,emergency_stop,verified_at,expires_at) VALUES('supervisor-'||$1::text,'supervisor',session_user,uuidv7(),false,clock_timestamp(),clock_timestamp()+interval '1 hour') RETURNING incarnation::text`, h.r).Scan(&supervisor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp() n) UPDATE scheduler_reservations SET namespace_incarnation=uuidv7(),supervisor_incarnation=$5,prepared_at=t.n,permit_deadline=t.n+interval '35 seconds',preparation_digest=decode(repeat('ab',32),'hex') FROM t WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4`, h.w, h.p, h.m, h.r, supervisor); err != nil {
		t.Fatal(err)
	}
}
func TestSchedulerStartRequiresPreparedSupervisorAndConsumesTokens(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	if _, err := schedulerClaim(t, w, inc, h); err != nil {
		t.Fatal(err)
	}
	if _, err := schedulerStart(t, w, inc, h); err == nil {
		t.Fatal("unprepared Start")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND attempt_state='leased'`, m.ID) != 1 {
		t.Fatal("unprepared start changed run")
	}
	schedulerPreparedDatabaseFixture(t, db, h)
	a := activationRead(t, db, f.od, m)
	if _, err := schedulerStart(t, w, inc, h); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND attempt_state='started' AND started_at IS NOT NULL`, m.ID) != 1 || queryInt(t, db, `SELECT tokens FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p) != 0 {
		t.Fatal("Start/token commit not atomic")
	}
	if _, err := schedulerStart(t, w, inc, h); err == nil {
		t.Fatal("second Start replay accepted")
	}
	after := activationRead(t, db, f.od, m)
	if monitor.ActivationETag(a) != monitor.ActivationETag(after) || !a.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatal("operational state changed intent tag")
	}
	// A pause after Start changes intent only; the old in-flight lineage remains.
	if _, err := activationPut(monitor.Store{Pool: db}, f.od, m, after, "disabled"); err != nil {
		t.Fatal(err)
	}
	status, err := (monitor.Store{Pool: db}).ExecutionStatus(ctx, f.od, h.w, h.p, h.m)
	if err != nil || status.DesiredState != "disabled" || status.SchedulingState != "disabled" || !status.InFlight || status.LatestAttempt == nil || status.LatestAttempt.AttemptState != "started" {
		t.Fatal("pause hid prior Start", err, status)
	}
}
func TestSchedulerPauseBeforeStartAndWhileWaiting(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	for _, order := range []string{"before", "waiting"} {
		t.Run(order, func(t *testing.T) {
			f, m, h := schedulerClaimFixture(t, db)
			if _, err := schedulerClaim(t, w, inc, h); err != nil {
				t.Fatal(err)
			}
			schedulerPreparedDatabaseFixture(t, db, h)
			a := activationRead(t, db, f.od, m)
			if order == "before" {
				if _, err := activationPut(monitor.Store{Pool: db}, f.od, m, a, "disabled"); err != nil {
					t.Fatal(err)
				}
				if _, err := schedulerStart(t, w, inc, h); err == nil {
					t.Fatal("pause-before Start authorized")
				}
				return
			}
			pause := newWorkspacePause(t)
			pid := make(chan int, 1)
			paused := make(chan error, 1)
			go func() {
				_, err := (monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
					return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
						if stage == "activation" {
							pause.stop(ctx)
						}
					}}
				}}}).ActivationUpdate(ctx, f.od, h.w, h.p, h.m, monitor.ActivationInput{DesiredState: "disabled", ConfigurationVersion: "1"}, monitor.ActivationETag(a))
				paused <- err
			}()
			pp := <-pid
			pause.wait(t)
			conn, err := w.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			sp := int(conn.Conn().PgConn().PID())
			started := make(chan error, 1)
			go func() {
				e := func() error {
					tx, e := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
					if e != nil {
						return e
					}
					defer tx.Rollback(ctx)
					var raw []byte
					e = tx.QueryRow(ctx, schedulerStartSQL, inc, h.w, h.p, h.m, h.u, h.b, h.r, "1").Scan(&raw)
					if e == nil {
						e = tx.Commit(ctx)
					}
					return e
				}()
				conn.Release()
				started <- e
			}()
			waitActivationBlock(t, db, sp)
			schedulerLockReceipt(t, db, "Start waits on pause-held Workspace", pp, sp)
			pause.resume()
			if err = <-paused; err != nil {
				t.Fatal(err)
			}
			if err = <-started; err == nil {
				t.Fatal("Start used pre-pause snapshot")
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND started_at IS NOT NULL`, m.ID) != 0 {
				t.Fatal("paused Start committed")
			}
		})
	}
}
