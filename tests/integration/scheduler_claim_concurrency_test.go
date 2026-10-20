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

type schedulerClaimResult struct {
	raw []byte
	err error
}

func startSchedulerClaim(t *testing.T, pool *pgxpool.Pool, inc string, h schedulerClaimHint) (int, <-chan schedulerClaimResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	pid := int(conn.Conn().PgConn().PID())
	ch := make(chan schedulerClaimResult, 1)
	go func() {
		defer cancel()
		result := func() schedulerClaimResult {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				return schedulerClaimResult{err: err}
			}
			defer tx.Rollback(context.Background())
			var raw []byte
			err = tx.QueryRow(ctx, schedulerClaimSQL, h.args(inc)...).Scan(&raw)
			if err == nil {
				err = tx.Commit(ctx)
			}
			return schedulerClaimResult{raw, err}
		}()
		conn.Release()
		ch <- result
	}()
	return pid, ch
}
func schedulerLockReceipt(t *testing.T, db *pgxpool.Pool, label string, pids ...int) {
	t.Helper()
	var raw []byte
	if err := db.QueryRow(context.Background(), `SELECT jsonb_build_object('event',$1::text,'database_time',clock_timestamp(),'backends',(SELECT jsonb_agg(jsonb_build_object('pid',a.pid,'state',a.state,'wait_event',a.wait_event,'xact_start',a.xact_start,'blocking_pids',pg_blocking_pids(a.pid),'locks',(SELECT jsonb_agg(jsonb_build_object('type',l.locktype,'relation',l.relation::regclass::text,'mode',l.mode,'granted',l.granted,'transactionid',l.transactionid::text)) FROM pg_locks l WHERE l.pid=a.pid))) FROM pg_stat_activity a WHERE a.pid=ANY($2::integer[])))`, label, pids).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		t.Fatal("invalid receipt")
	}
	t.Log(string(raw))
}
func awaitSchedulerClaim(t *testing.T, ch <-chan schedulerClaimResult, wantSuccess bool) []byte {
	t.Helper()
	select {
	case r := <-ch:
		if (r.err == nil) != wantSuccess {
			t.Fatal("claim outcome", r.err)
		}
		return r.raw
	case <-time.After(3 * time.Second):
		t.Fatal("claim failed to finish")
		return nil
	}
}
func TestSchedulerClaimTwoOwnersWorkersAndUserPatch(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m, h := schedulerClaimFixture(t, db)
	if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
		t.Fatal(err)
	}
	h2 := h
	h2.u = f.member.ID
	if err := db.QueryRow(ctx, `SELECT membership_id::text FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID).Scan(&h2.b); err != nil {
		t.Fatal(err)
	}
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	schedulerDatabaseOnlyGate(t, db)
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' AND budget_key='global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	pid1, ch1 := startSchedulerClaim(t, w1, i1, h)
	waitActivationBlock(t, db, pid1)
	// PATCH is a real user writer: U2/Session are protected before it waits W.
	patchPID := make(chan int, 1)
	patch := make(chan error, 1)
	go func() {
		_, e := (monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: patchPID}}).Update(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("patched while claiming"), monitor.ETag(m))
		patch <- e
	}()
	pp := <-patchPID
	waitActivationBlock(t, db, pp)
	schedulerLockReceipt(t, db, "worker1 holds U1/W through Run; U2 PATCH waits W", pid1, pp)
	pid2, ch2 := startSchedulerClaim(t, w2, i2, h2)
	awaitSchedulerClaim(t, ch2, false)
	schedulerLockReceipt(t, db, "worker2 with U2 skipped W and rolled back", pid1, pp, pid2)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	awaitSchedulerClaim(t, ch1, true)
	if err = <-patch; err != nil {
		t.Fatal("PATCH cannot progress", err)
	}
	schedulerLockReceipt(t, db, "claim then PATCH commits", pid1, pp, pid2)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND fencing_token=1`, m.ID) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND last_fencing_token=1 AND authorized_activation_generation IS NULL`, m.ID) != 1 {
		t.Fatal("claim/PATCH ordering lost lineage")
	}
}
func TestSchedulerClaimOwnerHintInvalidation(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	for _, mode := range []string{"disabled", "demoted", "removed", "rejoined-member", "rejoined-owner"} {
		t.Run(mode, func(t *testing.T) {
			f, m, h := schedulerClaimFixture(t, db)
			switch mode {
			case "disabled":
				if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID); err != nil {
					t.Fatal(err)
				}
			case "demoted":
				if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID); err != nil {
					t.Fatal(err)
				}
			default:
				if _, err := db.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID); err != nil {
					t.Fatal(err)
				}
				role := "member"
				if mode == "rejoined-owner" {
					role = "owner"
				}
				if mode != "removed" {
					if _, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, f.w.ID, f.owner.ID, role); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := schedulerClaim(t, w1, i1, h); err == nil {
				t.Fatal("stale owner hint granted claim")
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, m.ID) != 0 {
				t.Fatal("invalid owner left provisional run")
			}
			if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
				t.Fatal(err)
			}
			h.u = f.member.ID
			if err := db.QueryRow(ctx, `SELECT membership_id::text FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID).Scan(&h.b); err != nil {
				t.Fatal(err)
			}
			if _, err := schedulerClaim(t, w1, i1, h); err != nil {
				t.Fatal("fresh transaction could not choose different owner", err)
			}
		})
	}
}
func TestSchedulerClaimFinalClockAfterControlWait(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, m, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	// Administrative short-deadline fixture keeps the API's >=60s public contract.
	var expiry time.Time
	if err := db.QueryRow(ctx, `UPDATE monitor_schedules SET expires_at=clock_timestamp()+interval '200ms',scheduler_epoch=scheduler_epoch+1 WHERE monitor_id=$1 RETURNING expires_at,scheduler_epoch::text`, m.ID).Scan(&expiry, &h.e); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT control_id FROM scheduler_control WHERE control_id='deployment' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	pid, ch := startSchedulerClaim(t, w, inc, h)
	waitActivationBlock(t, db, pid)
	schedulerLockReceipt(t, db, "claim final control wait before authorization expires", pid)
	waitWorkspaceTime(t, db, expiry)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-ch:
		var rejection *pgconn.PgError
		if !errors.As(result.err, &rejection) || rejection.Code != "P0001" || rejection.Message != "candidate_invalid" {
			t.Fatal("expected post-wait authorization rejection, not lock/transaction timeout", result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not finish after expiry barrier")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, m.ID) != 0 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global' AND budget_key='global'`) != 0 {
		t.Fatal("expired claim committed stale clock or partial capacity")
	}
}
