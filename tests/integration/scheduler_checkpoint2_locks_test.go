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

// These probes use independent connections and NOWAIT, never elapsed-time
// inference, to distinguish locks already acquired from later lock stages.
func checkpointTwoProbe(t *testing.T, db *pgxpool.Pool, sql string, locked bool, args ...any) {
	t.Helper()
	tx, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(context.Background(), sql, args...)
	var p *pgconn.PgError
	if locked {
		if !errors.As(err, &p) || p.Code != "55P03" {
			t.Fatalf("lock probe: expected 55P03, got %v", err)
		}
	} else if err != nil {
		t.Fatalf("unexpected lock: %v", err)
	}
}
func checkpointTwoOutcome(t *testing.T, result schedulerClaimResult, code string) {
	t.Helper()
	var p *pgconn.PgError
	if code == "00000" {
		if result.err != nil {
			t.Fatal(result.err)
		}
	} else if !errors.As(result.err, &p) || p.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, result.err)
	}
	t.Logf("outcome database SQLSTATE=%s message=%v lease=%s ack_observed_db=%s", code, result.err, result.raw, result.ackObservedAt.Format(time.RFC3339Nano))
}
func checkpointTwoFinal(t *testing.T, db *pgxpool.Pool, h schedulerClaimHint) {
	t.Helper()
	// Configuration is read separately from URL-bearing monitor data.
	var version string
	if err := db.QueryRow(context.Background(), `SELECT version::text FROM http_monitors WHERE workspace_id=$1 AND project_id=$2 AND id=$3`, h.w, h.p, h.m).Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("final configuration_version=%s scope=%s/%s/%s", version, h.w, h.p, h.m)
	var raw []byte
	err := db.QueryRow(context.Background(), `SELECT jsonb_build_object('event','final_state','at',clock_timestamp(),'schedule',to_jsonb(s),'activation',to_jsonb(a),'runs',(SELECT jsonb_agg(to_jsonb(r)) FROM monitor_runs r WHERE r.workspace_id=s.workspace_id AND r.project_id=s.project_id AND r.monitor_id=s.monitor_id)) FROM monitor_schedules s JOIN monitor_activation_states a USING(workspace_id,project_id,monitor_id) WHERE s.workspace_id=$1 AND s.project_id=$2 AND s.monitor_id=$3`, h.w, h.p, h.m).Scan(&raw)
	if err != nil || !json.Valid(raw) {
		t.Fatal("final receipt", err)
	}
	t.Log(string(raw))
}
func checkpointTwoPromote(t *testing.T, db *pgxpool.Pool, f memberFixture, h schedulerClaimHint) schedulerClaimHint {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, h.w, f.member.ID); err != nil {
		t.Fatal(err)
	}
	h.u = f.member.ID
	if err := db.QueryRow(context.Background(), `SELECT membership_id::text FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, h.w, h.u).Scan(&h.b); err != nil {
		t.Fatal(err)
	}
	return h
}
func TestCheckpointTwoClaimCompetingOwners(t *testing.T) {
	for _, candidate := range []string{"same-owner", "different-owner"} {
		t.Run(candidate, func(t *testing.T) {
			db := schedulerHistoricalDB(t)
			ctx := context.Background()
			f, _, h := schedulerClaimFixture(t, db)
			h2 := h
			if candidate == "different-owner" {
				h2 = checkpointTwoPromote(t, db, f, h)
			}
			w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
			w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
			schedulerDatabaseOnlyGate(t, db)
			barrier, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer barrier.Rollback(ctx)
			if _, err = barrier.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			p1, c1 := startSchedulerClaim(t, w1, i1, h)
			waitActivationBlock(t, db, p1)
			// At the budget barrier, all parents and the provisional Run precede budgets.
			checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, true, h.u)
			checkpointTwoProbe(t, db, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, true, h.w)
			checkpointTwoProbe(t, db, `SELECT monitor_id FROM monitor_activation_states WHERE monitor_id=$1 FOR UPDATE NOWAIT`, true, h.m)
			if h.u != h2.u {
				checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, h2.u)
			}
			p2, c2 := startSchedulerClaim(t, w2, i2, h2)
			r2 := <-c2
			checkpointTwoOutcome(t, r2, "P0001")
			if queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND locktype IN ('transactionid','tuple')`, p2) != 0 {
				t.Fatal("skip retained transaction locks")
			}
			schedulerLockReceipt(t, db, "second worker skipped W; first never acquired alternate Owner", p1, p2)
			if err = barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			checkpointTwoOutcome(t, <-c1, "00000")
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND fencing_token=1`, h.m) != 1 {
				t.Fatal("competing claims did not preserve one fence")
			}
			checkpointTwoFinal(t, db, h)
		})
	}
}

func TestCheckpointTwoClaimOwnerLossBothOrders(t *testing.T) {
	for _, mode := range []string{"disabled", "demoted", "removed", "rejoined-member", "rejoined-owner"} {
		for _, order := range []string{"loss-first", "claim-first"} {
			t.Run(mode+"/"+order, func(t *testing.T) {
				db := schedulerHistoricalDB(t)
				ctx := context.Background()
				f, _, h := schedulerClaimFixture(t, db)
				h2 := checkpointTwoPromote(t, db, f, h)
				w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
				w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
				schedulerDatabaseOnlyGate(t, db)
				// Mutation follows the real parent direction, using a separate connection.
				mutate := func(tx pgx.Tx) error {
					if mode == "disabled" {
						_, e := tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, h.u)
						return e
					}
					if _, e := tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, h.w); e != nil {
						return e
					}
					if mode == "demoted" {
						_, e := tx.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, h.w, h.u)
						return e
					}
					if _, e := tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, h.w, h.u); e != nil {
						return e
					}
					if mode == "removed" {
						return nil
					}
					role := "member"
					if mode == "rejoined-owner" {
						role = "owner"
					}
					_, e := tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, h.w, h.u, role)
					return e
				}
				loss, err := db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer loss.Rollback(ctx)
				var lossPID int
				if err = loss.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&lossPID); err != nil {
					t.Fatal(err)
				}
				if order == "loss-first" {
					if err = mutate(loss); err != nil {
						t.Fatal(err)
					}
					p, c := startSchedulerClaim(t, w1, i1, h)
					if mode == "disabled" {
						waitActivationBlock(t, db, p)
						schedulerLockReceipt(t, db, "Claim waits candidate User before taking W", p, lossPID)
						if err = loss.Commit(ctx); err != nil {
							t.Fatal(err)
						}
						checkpointTwoOutcome(t, <-c, "P0001")
					} else {
						checkpointTwoOutcome(t, <-c, "P0001")
						schedulerLockReceipt(t, db, "Claim skips W owned by membership mutation", p, lossPID)
						if err = loss.Commit(ctx); err != nil {
							t.Fatal(err)
						}
					}
					// Even if rejoined as Owner, the old membership identity is invalid.
					_, err = schedulerClaim(t, w1, i1, h)
					checkpointInvalidCandidate(t, err)
					checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, h.u)
					if _, err = schedulerClaim(t, w2, i2, h2); err != nil {
						t.Fatal("fresh transaction with U2", err)
					}
				} else {
					budget, err := db.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer budget.Rollback(ctx)
					if _, err = budget.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
						t.Fatal(err)
					}
					p, c := startSchedulerClaim(t, w1, i1, h)
					waitActivationBlock(t, db, p)
					done := make(chan error, 1)
					go func() { done <- mutate(loss) }()
					waitActivationBlock(t, db, lossPID)
					schedulerLockReceipt(t, db, "Owner mutation waits Claim parent locks", p, lossPID)
					if err = budget.Commit(ctx); err != nil {
						t.Fatal(err)
					}
					checkpointTwoOutcome(t, <-c, "00000")
					if err = <-done; err != nil {
						t.Fatal(err)
					}
					if err = loss.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1 AND fencing_token=1 AND attempt_state='leased'`, h.m) != 1 {
					t.Fatal("wrong final lineage")
				}
				checkpointTwoFinal(t, db, h)
			})
		}
	}
}

func TestCheckpointTwoPatchBeforeWorkers(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m, h := schedulerClaimFixture(t, db)
	h2 := checkpointTwoPromote(t, db, f, h)
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	schedulerDatabaseOnlyGate(t, db)
	pause := newWorkspacePause(t)
	patchPID := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		_, err := (monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: patchPID, wrap: func(tx pgx.Tx) pgx.Tx {
			return workspaceObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
				if stage == "commit" {
					pause.stop(ctx)
				}
			}}
		}}}).Update(ctx, f.md, h.w, h.p, h.m, monitorInput("patch first"), monitor.ETag(m))
		done <- err
	}()
	pp := <-patchPID
	pause.wait(t)
	for i, item := range []struct {
		pool *pgxpool.Pool
		inc  string
		hint schedulerClaimHint
	}{{w1, i1, h}, {w2, i2, h2}} {
		pid, ch := startSchedulerClaim(t, item.pool, item.inc, item.hint)
		checkpointTwoOutcome(t, <-ch, "P0001")
		schedulerLockReceipt(t, db, "PATCH owns W; worker skips and releases User", pp, pid)
		if queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND locktype IN ('transactionid','tuple')`, pid) != 0 {
			t.Fatalf("worker%d retained locks", i+1)
		}
	}
	pause.resume()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != 0 || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND authorized_activation_generation IS NULL AND last_fencing_token=0`, h.m) != 1 {
		t.Fatal("PATCH-first stale claim")
	}
	checkpointTwoAssertPatched(t, db, h)
	checkpointTwoFinal(t, db, h)
}

func checkpointTwoAssertPatched(t *testing.T, db *pgxpool.Pool, h schedulerClaimHint) {
	t.Helper()
	if queryInt(t, db, `SELECT count(*) FROM http_monitors m JOIN monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id JOIN monitor_schedules s ON s.workspace_id=m.workspace_id AND s.project_id=m.project_id AND s.monitor_id=m.id WHERE m.workspace_id=$1 AND m.project_id=$2 AND m.id=$3 AND m.version=2 AND a.activation_generation=3 AND s.scheduler_epoch=2 AND s.authorized_configuration_version IS NULL AND s.next_due_at IS NULL`, h.w, h.p, h.m) != 1 {
		t.Fatal("PATCH final v/g/epoch or authorization mismatch")
	}
}

func TestCheckpointTwoDifferentWorkspaceCandidates(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	var hints []schedulerClaimHint
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) { _, _, h := schedulerClaimFixture(t, db); hints = append(hints, h) })
	}
	if len(hints) != 2 {
		t.Fatal("missing fixture")
	}
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	schedulerDatabaseOnlyGate(t, db)
	barrier, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(ctx)
	if _, err = barrier.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	p1, c1 := startSchedulerClaim(t, w1, i1, hints[0])
	waitActivationBlock(t, db, p1)
	p2, c2 := startSchedulerClaim(t, w2, i2, hints[1])
	waitActivationBlock(t, db, p2)
	schedulerLockReceipt(t, db, "different scopes hold their own User/W/parents and serialize at global budget", p1, p2)
	if err = barrier.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	checkpointTwoOutcome(t, <-c1, "00000")
	checkpointTwoOutcome(t, <-c2, "00000")
	for _, h := range hints {
		checkpointTwoFinal(t, db, h)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE fencing_token=1`) != 2 {
		t.Fatal("independent scopes lost fence")
	}
}

func TestCheckpointTwoDifferentMonitorSameWorkspace(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m, h := schedulerClaimFixture(t, db)
	h2 := checkpointTwoPromote(t, db, f, h)
	next, err := (monitor.Store{Pool: db}).Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("other candidate"))
	if err != nil {
		t.Fatal(err)
	}
	a := activationRead(t, db, f.od, next)
	if _, err = (monitor.Store{Pool: db}).ExecutionAuthorize(ctx, f.od, next.WorkspaceID, next.ProjectID, next.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a)); err != nil {
		t.Fatal(err)
	}
	h2.m = next.ID
	if err = db.QueryRow(ctx, `SELECT authorized_configuration_version::text,authorized_activation_generation::text,scheduler_epoch::text,next_run_id::text FROM monitor_schedules WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h2.w, h2.p, h2.m).Scan(&h2.v, &h2.g, &h2.e, &h2.r); err != nil {
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
	if _, err = blocker.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	p1, c1 := startSchedulerClaim(t, w1, i1, h)
	waitActivationBlock(t, db, p1)
	p2, c2 := startSchedulerClaim(t, w2, i2, h2)
	checkpointTwoOutcome(t, <-c2, "P0001")
	schedulerLockReceipt(t, db, "different monitor/Owner candidates serialize at W before children", p1, p2)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	checkpointTwoOutcome(t, <-c1, "00000")
	if _, err = schedulerClaim(t, w2, i2, h2); err != nil {
		t.Fatal("fresh candidate transaction cannot progress", err)
	}
	checkpointTwoFinal(t, db, h)
	checkpointTwoFinal(t, db, h2)
}
