//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
)

func TestCheckpointOneLegacyAndAuthorizationSerialize(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	// This is a test profile, not an assertion about deployed historical ACLs.
	if _, err := db.Exec(ctx, `CREATE ROLE checkpoint1_legacy LOGIN PASSWORD 'disposable_fixture_only';GRANT USAGE ON SCHEMA public TO checkpoint1_legacy;GRANT SELECT,UPDATE ON users,sessions,workspaces,workspace_members,projects,http_monitors,monitor_activation_states TO checkpoint1_legacy`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DROP OWNED BY checkpoint1_legacy;DROP ROLE checkpoint1_legacy`); err != nil {
			t.Error(err)
		}
	})
	cfg := db.Config()
	cfg.ConnConfig.User = "checkpoint1_legacy"
	cfg.ConnConfig.Password = "disposable_fixture_only"
	old, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(old.Close)
	for _, first := range []string{"legacy", "authorization"} {
		t.Run(first, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			a := activationRead(t, db, f.od, m)
			tag := monitor.ActivationETag(a)
			pause := newWorkspacePause(t)
			pid := make(chan int, 1)
			done1 := make(chan error, 1)
			pid2 := make(chan int, 1)
			done2 := make(chan error, 1)
			call := func(kind string, observed workspaceObservedDB) error {
				if kind == "legacy" {
					_, err := (monitor.SchemaSevenFixtureStore{Pool: observed}).ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "enabled", ConfigurationVersion: "1"}, tag)
					return err
				}
				_, err := (monitor.Store{Pool: observed}).ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, tag)
				return err
			}
			pool := db
			other := "legacy"
			otherPool := old
			if first == "legacy" {
				pool = old
				other = "authorization"
				otherPool = db
			}
			go func() {
				done1 <- call(first, workspaceObservedDB{Pool: pool, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
					return workspaceObservedTx{Tx: tx, after: func(c context.Context, stage string) {
						if stage == "commit" {
							pause.stop(c)
						}
					}}
				}})
			}()
			p1 := <-pid
			pause.wait(t)
			go func() { done2 <- call(other, workspaceObservedDB{Pool: otherPool, pid: pid2}) }()
			p2 := <-pid2
			waitActivationBlock(t, db, p2)
			schedulerLockReceipt(t, db, first+" owns parent chain before commit", p1, p2)
			pause.resume()
			monitorWant(t, <-done1, nil)
			monitorWant(t, <-done2, monitor.ErrPreconditionFailed)
			want := 0
			if first == "authorization" {
				want = 1
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND authorized_activation_generation IS NOT NULL`, m.ID) != want {
				t.Fatal("legacy granted or destroyed another commit without a fresh tag")
			}
			if first == "authorization" {
				a = activationRead(t, db, f.od, m)
				if _, err := (monitor.SchemaSevenFixtureStore{Pool: old}).ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "disabled", ConfigurationVersion: "1"}, monitor.ActivationETag(a)); err != nil {
					t.Fatal(err)
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND authorized_activation_generation IS NULL AND next_due_at IS NULL AND scheduler_epoch=2`, m.ID) != 1 {
					t.Fatal("fresh legacy pause failed downward revocation")
				}
			}
		})
	}
}

func TestCheckpointOneHTTPInventoryBlocksDatabaseAdmission(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, m, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	if _, err := db.Exec(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,routable,status_ready) VALUES('old-http','http',session_user,uuidv7(),true,false)`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"old", "new-status-unready", "expired"} {
		t.Run(mode, func(t *testing.T) {
			sql := `UPDATE scheduler_control SET projection_protocol=NULL,status_ready=false,expires_at=NULL WHERE control_id='old-http'`
			if mode == "new-status-unready" {
				sql = `UPDATE scheduler_control SET projection_protocol='intent-v2',status_ready=false,expires_at=clock_timestamp()+interval '1 hour' WHERE control_id='old-http'`
			}
			if mode == "expired" {
				sql = `UPDATE scheduler_control SET projection_protocol='intent-v2',status_ready=true,expires_at=clock_timestamp()-interval '1 second' WHERE control_id='old-http'`
			}
			if _, err := db.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			_, err := schedulerClaim(t, w, inc, h)
			checkpointInvalidCandidate(t, err)
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, m.ID) != 0 {
				t.Fatal("unready HTTP inventory permitted Claim")
			}
		})
	}
	if _, err := db.Exec(ctx, `UPDATE scheduler_control SET projection_protocol='intent-v2',status_ready=true,expires_at=clock_timestamp()+interval '1 hour' WHERE control_id='old-http'`); err != nil {
		t.Fatal(err)
	}
	if _, err := schedulerClaim(t, w, inc, h); err != nil {
		t.Fatal("verified database inventory positive control", err)
	}
	schedulerPreparedDatabaseFixture(t, db, h)
	if _, err := db.Exec(ctx, `UPDATE scheduler_control SET projection_protocol=NULL WHERE control_id='old-http'`); err != nil {
		t.Fatal(err)
	}
	_, err := schedulerStart(t, w, inc, h)
	checkpointInvalidCandidate(t, err)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND started_at IS NOT NULL`, m.ID) != 0 {
		t.Fatal("old HTTP returned before Start")
	}
}
