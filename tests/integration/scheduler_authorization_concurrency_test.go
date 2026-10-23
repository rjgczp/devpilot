//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func TestExecutionAuthorizationFreshTimeAfterEverySPIWait(t *testing.T) {
	db := workspaceDB(t)
	for _, stage := range []string{"user", "session", "workspace", "membership", "project", "monitor", "activation", "schedule", "budget", "control"} {
		t.Run(stage, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			a := activationRead(t, db, f.od, m)
			var expiry time.Time
			if err := db.QueryRow(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '200ms' WHERE token_hash=$1 RETURNING expires_at`, f.od[:]).Scan(&expiry); err != nil {
				t.Fatal(err)
			}
			sql := `SELECT id FROM users WHERE id=$1 FOR UPDATE`
			arg := any(f.owner.ID)
			switch stage {
			case "session":
				sql = `SELECT id FROM sessions WHERE token_hash=$1 FOR UPDATE`
				arg = f.od[:]
			case "workspace":
				sql = `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`
				arg = f.w.ID
			case "membership":
				sql = `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND role='owner' FOR UPDATE`
				arg = f.w.ID
			case "project":
				sql = `SELECT id FROM projects WHERE id=$1 FOR UPDATE`
				arg = m.ProjectID
			case "monitor":
				sql = `SELECT id FROM http_monitors WHERE id=$1 FOR UPDATE`
				arg = m.ID
			case "activation":
				sql = `SELECT monitor_id FROM monitor_activation_states WHERE monitor_id=$1 FOR UPDATE`
				arg = m.ID
			case "schedule":
				if _, err := db.Exec(ctx, `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, m.WorkspaceID, m.ProjectID, m.ID); err != nil {
					t.Fatal(err)
				}
				sql = `SELECT monitor_id FROM monitor_schedules WHERE monitor_id=$1 FOR UPDATE`
				arg = m.ID
			case "budget":
				sql = `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' AND budget_key=$1 FOR UPDATE`
				arg = "global"
			case "control":
				sql = `SELECT control_id FROM scheduler_control WHERE control_id=$1 FOR UPDATE`
				arg = "deployment"
			}
			blocker, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err = blocker.Exec(ctx, sql, arg); err != nil {
				t.Fatal(err)
			}
			pid := make(chan int, 1)
			store := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}
			ch := make(chan error, 1)
			go func() {
				_, e := store.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
				ch <- e
			}()
			actualPID := <-pid
			waitActivationBlock(t, db, actualPID)
			var locks []byte
			if err = db.QueryRow(ctx, `SELECT jsonb_build_object('pid',$1::int,'blocking',pg_blocking_pids($1),'locks',(SELECT jsonb_agg(jsonb_build_object('locktype',locktype,'relation',relation::regclass::text,'mode',mode,'granted',granted)) FROM pg_locks WHERE pid=$1),'observed_at',clock_timestamp())`, actualPID).Scan(&locks); err != nil {
				t.Fatal(err)
			}
			var valid any
			if json.Unmarshal(locks, &valid) != nil {
				t.Fatal("invalid lock receipt")
			}
			t.Log("wait evidence", string(locks))
			waitWorkspaceTime(t, db, expiry)
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-ch:
				monitorWant(t, err, session.ErrInvalid)
			case <-ctx.Done():
				t.Fatal("authorization did not finish")
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_activation_states WHERE monitor_id=$1 AND activation_generation=1 AND desired_state='disabled'`, m.ID) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND expires_at IS NOT NULL`, m.ID) != 0 {
				t.Fatal("expired caller persisted authorization")
			}
		})
	}
}

func TestExecutionAuthorizationTwoOwnersSameTag(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
		t.Fatal(err)
	}
	a := activationRead(t, db, f.od, m)
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID); err != nil {
		t.Fatal(err)
	}
	pid := make(chan int, 2)
	s := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}
	result := make(chan error, 2)
	for _, digest := range []session.Digest{f.od, f.md} {
		go func(d session.Digest) {
			_, e := s.ExecutionAuthorize(ctx, d, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
			result <- e
		}(digest)
	}
	for range 2 {
		p := <-pid
		waitActivationBlock(t, db, p)
		var receipt []byte
		if err = db.QueryRow(ctx, `SELECT jsonb_build_object('pid',$1::int,'blocking',pg_blocking_pids($1),'at',clock_timestamp(),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l WHERE l.pid=$1))`, p).Scan(&receipt); err != nil {
			t.Fatal(err)
		}
		t.Log("two-owner barrier", string(receipt))
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	ok, stale := 0, 0
	for range 2 {
		err = <-result
		if err == nil {
			ok++
		} else if errors.Is(err, monitor.ErrPreconditionFailed) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || stale != 1 || activationRead(t, db, f.od, m).ActivationGeneration != "2" || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND authorized_activation_generation=2`, m.ID) != 1 {
		t.Fatal("same tag granted twice", ok, stale)
	}
}

func TestExecutionAuthorizationLossWhileWaiting(t *testing.T) {
	db := workspaceDB(t)
	for _, change := range []string{"disabled", "logout", "demoted", "removed", "rejoined-member"} {
		t.Run(change, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			a := activationRead(t, db, f.od, m)
			blocker, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			want := monitor.ErrForbidden
			switch change {
			case "disabled":
				_, err = blocker.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID)
				want = session.ErrInvalid
			case "logout":
				_, err = blocker.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_hash=$1`, f.od[:])
				want = session.ErrInvalid
			default:
				_, err = blocker.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID)
				if err == nil {
					if change == "demoted" {
						_, err = blocker.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID)
					} else {
						_, err = blocker.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID)
						if change == "removed" {
							want = monitor.ErrNotFound
						}
						if err == nil && change == "rejoined-member" {
							_, err = blocker.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.w.ID, f.owner.ID)
						}
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			pid := make(chan int, 1)
			result := make(chan error, 1)
			go func() {
				_, e := (monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}).ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
				result <- e
			}()
			waitActivationBlock(t, db, <-pid)
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			monitorWant(t, <-result, want)
			if queryInt(t, db, `SELECT count(*) FROM monitor_activation_states WHERE monitor_id=$1 AND activation_generation=1 AND desired_state='disabled'`, m.ID) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1`, m.ID) != 0 {
				t.Fatal("lost owner persisted grant")
			}
		})
	}
}

func TestExecutionAuthorizationCommitAcknowledgementUnknown(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, outcome := range []string{"rollback", "lost"} {
		t.Run(outcome, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			a := activationRead(t, db, f.od, m)
			var count atomic.Int32
			s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: outcome, commits: &count} }}}
			got, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
			monitorWant(t, err, user.ErrUnavailable)
			if got.MonitorID != "" || count.Load() != 1 {
				t.Fatal("unconfirmed success or retry")
			}
			want := 0
			if outcome == "lost" {
				want = 1
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND expires_at IS NOT NULL`, m.ID) != want {
				t.Fatal("atomic schedule/intent unknown outcome")
			}
			status, err := (monitor.Store{Pool: db}).ExecutionStatus(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			if outcome == "lost" && status.AuthorizationState != "valid" {
				t.Fatal("committed authorization not reconciled")
			}
		})
	}
}
