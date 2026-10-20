//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
)

// Executes the existing Start function only to validate Checkpoint1 authority.
// There is no Probe/namespace/network permit in these database-only fixtures.
func checkpointStartAsync(t *testing.T, pool *pgxpool.Pool, inc string, h schedulerClaimHint) (int, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	pid := int(conn.Conn().PgConn().PID())
	done := make(chan error, 1)
	go func() {
		defer cancel()
		err := func() error {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				return err
			}
			defer tx.Rollback(context.Background())
			var raw []byte
			if err = tx.QueryRow(ctx, schedulerStartSQL, inc, h.w, h.p, h.m, h.u, h.b, h.r, "1").Scan(&raw); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}()
		conn.Release()
		done <- err
	}()
	return pid, done
}

func checkpointInvalidCandidate(t *testing.T, err error) {
	t.Helper()
	var p *pgconn.PgError
	if !errors.As(err, &p) || p.Code != "P0001" || p.Message != "candidate_invalid" {
		t.Fatalf("expected authority rejection, not timeout/deadlock: %v", err)
	}
	t.Logf("final gate rejected SQLSTATE=%s reason=%s", p.Code, p.Message)
}

func TestCheckpointOneStartOwnerLossBothOrders(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	for _, mode := range []string{"disabled", "demoted", "removed", "rejoined-member", "rejoined-owner"} {
		for _, order := range []string{"loss-first-wait", "start-first"} {
			t.Run(mode+"/"+order, func(t *testing.T) {
				f, m, h := schedulerClaimFixture(t, db)
				if _, err := schedulerClaim(t, w, inc, h); err != nil {
					t.Fatal(err)
				}
				schedulerPreparedDatabaseFixture(t, db, h)
				before := activationRead(t, db, f.od, m)
				if order == "start-first" {
					if _, err := schedulerStart(t, w, inc, h); err != nil {
						t.Fatal(err)
					}
				}
				tx, err := db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if mode == "disabled" {
					_, err = tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID)
				} else {
					_, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID)
					if err == nil {
						if mode == "demoted" {
							_, err = tx.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID)
						} else {
							_, err = tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID)
							if err == nil && mode != "removed" {
								role := "member"
								if mode == "rejoined-owner" {
									role = "owner"
								}
								_, err = tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, f.w.ID, f.owner.ID, role)
							}
						}
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				var done <-chan error
				if order == "loss-first-wait" {
					pid, ch := checkpointStartAsync(t, w, inc, h)
					done = ch
					waitActivationBlock(t, db, pid)
					schedulerLockReceipt(t, db, "Owner mutation precedes waiting Start", pid)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if done != nil {
					checkpointInvalidCandidate(t, <-done)
				}
				want := 0
				if order == "start-first" {
					want = 1
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND started_at IS NOT NULL`, m.ID) != want {
					t.Fatal("Start authority ordering")
				}
				after := activationRead(t, db, f.md, m)
				if monitor.ActivationETag(before) != monitor.ActivationETag(after) {
					t.Fatal("Owner set mutation changed workspace intent")
				}
				status, err := (monitor.Store{Pool: db}).ExecutionStatus(ctx, f.md, h.w, h.p, h.m)
				if err != nil {
					t.Fatal(err)
				}
				if mode != "rejoined-owner" && (status.BlockedReason == nil || *status.BlockedReason != "owner_unavailable" || status.EffectiveAuthorization) {
					t.Fatal("no active Owner not projected", status)
				}
			})
		}
	}
}

func TestCheckpointOneStartExpiryAfterFinalWait(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, m, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	// Administrative short deadline before Claim; the public API still uses60s.
	var expiry time.Time
	if err := db.QueryRow(ctx, `UPDATE monitor_schedules SET expires_at=clock_timestamp()+interval '200ms',scheduler_epoch=scheduler_epoch+1 WHERE monitor_id=$1 RETURNING expires_at,scheduler_epoch::text`, m.ID).Scan(&expiry, &h.e); err != nil {
		t.Fatal(err)
	}
	if _, err := schedulerClaim(t, w, inc, h); err != nil {
		t.Fatal(err)
	}
	schedulerPreparedDatabaseFixture(t, db, h)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT control_id FROM scheduler_control WHERE control_id='deployment' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	pid, done := checkpointStartAsync(t, w, inc, h)
	waitActivationBlock(t, db, pid)
	schedulerLockReceipt(t, db, "Start final control wait across expiry", pid)
	waitWorkspaceTime(t, db, expiry)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	checkpointInvalidCandidate(t, <-done)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND attempt_state='leased' AND started_at IS NULL`, m.ID) != 1 {
		t.Fatal("expired Start mutated run")
	}
	if queryInt(t, db, `SELECT tokens FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p) != 1000000 {
		t.Fatal("rejected Start consumed token")
	}
}

func TestCheckpointOneSessionIsNotBackgroundIdentity(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	for _, mode := range []string{"logout", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f, m, h := schedulerClaimFixture(t, db)
			before := activationRead(t, db, f.od, m)
			var expires time.Time
			if err := db.QueryRow(ctx, `SELECT expires_at FROM monitor_schedules WHERE monitor_id=$1`, m.ID).Scan(&expires); err != nil {
				t.Fatal(err)
			}
			if mode == "logout" {
				if err := (session.Store{Pool: db}).Revoke(ctx, f.od); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp() WHERE token_hash=$1`, f.od[:]); err != nil {
					t.Fatal(err)
				}
			}
			_, err := (monitor.Store{Pool: db}).ExecutionAuthorize(ctx, f.od, h.w, h.p, h.m, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(before))
			monitorWant(t, err, session.ErrInvalid)
			if _, err = schedulerClaim(t, w, inc, h); err != nil {
				t.Fatal("active Owner witness must not require former Session", err)
			}
			schedulerPreparedDatabaseFixture(t, db, h)
			if _, err = schedulerStart(t, w, inc, h); err != nil {
				t.Fatal("system Start borrowed former Session", err)
			}
			var after time.Time
			if err = db.QueryRow(ctx, `SELECT expires_at FROM monitor_schedules WHERE monitor_id=$1`, m.ID).Scan(&after); err != nil || !after.Equal(expires) {
				t.Fatal("session loss renewed/revoked workspace grant", err)
			}
			if monitor.ActivationETag(activationRead(t, db, f.md, m)) != monitor.ActivationETag(before) {
				t.Fatal("background changed intent tag")
			}
		})
	}
}
