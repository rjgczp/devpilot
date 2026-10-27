//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture

package integration

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"testing"
	"time"
)

const commitSQL = `SELECT scheduler_private.commit_result($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::uuid,$13::integer,$14::integer,$15::boolean,$16::text)`

func commitArgs(s scheduler.Store, l handoff.Lineage, r *monitoring.Result) []any {
	a := minimalArgs(s, l, r)
	a[11] = "33333333-3333-4333-8333-333333333333"
	return a[:16]
}
func commitCall(t *testing.T, s scheduler.Store, a []any, commit bool, want string) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var raw []byte
	e = tx.QueryRow(ctx, commitSQL, a...).Scan(&raw)
	budgetSQLState(t, e, want)
	if e == nil && commit {
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
}
func runCommit(t *testing.T, mode string) {
	defer func() { jointResultCheck = nil; jointResultPatch = nil; jointResultSetup = nil }()
	if mode == "renew" {
		jointResultSetup = func(t *testing.T, db *pgxpool.Pool, m monitor.Public) {
			var until time.Time
			if e := db.QueryRow(context.Background(), `SELECT next_enable_allowed_at FROM monitor_activation_states WHERE monitor_id=$1`, m.ID).Scan(&until); e != nil {
				t.Fatal(e)
			}
			t.Log("real cooldown before Claim", until)
			time.Sleep(time.Until(until) + 20*time.Millisecond)
		}
	}
	jointResultCheck = func(t *testing.T, stage string, db *pgxpool.Pool, s scheduler.Store, l handoff.Lineage, r *monitoring.Result) {
		ctx := context.Background()
		if stage == "claimed" {
			if mode == "patch" || mode == "pause" || mode == "renew" {
				jointResultPatch = func(t *testing.T, f memberFixture, m monitor.Public) {
					st := monitor.Store{Pool: db}
					a := activationRead(t, db, f.od, m)
					switch mode {
					case "patch":
						n := "new-version-result72"
						if _, e := st.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &n}, monitor.ETag(m)); e != nil {
							t.Fatal(e)
						}
					case "pause":
						if _, e := st.ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "disabled", ConfigurationVersion: m.Version}, monitor.ActivationETag(a)); e != nil {
							t.Fatal(e)
						}
					case "renew":
						if _, e := st.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: m.Version, ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 120}, monitor.ActivationETag(a)); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			return
		}
		if stage == "before-stop" {
			if r != nil {
				commitCall(t, s, commitArgs(s, l, r), false, "P0001")
			}
			return
		}
		if stage != "released" || r == nil {
			t.Fatal("expected real released network result")
		}
		v, e := scheduler.NetworkResult(l, r)
		if e != nil {
			t.Fatal(e)
		}
		c, e := scheduler.NewResultCommit(v)
		if e != nil {
			t.Fatal(e)
		}
		if mode == "validation" {
			a := commitArgs(s, l, r)
			var f2 memberFixture
			var m2 monitor.Public
			if !t.Run("other-scope-fixture", func(t *testing.T) { f2, m2 = monitorFixture(t, db); _ = budgetHint(t, db, f2, m2) }) {
				return
			}
			bad := append([]any(nil), a...)
			bad[1], bad[2], bad[3] = m2.WorkspaceID, m2.ProjectID, m2.ID
			commitCall(t, s, bad, false, "P0001")
			for _, sig := range []string{
				"scheduler_private.commit_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,uuid,integer,integer,boolean,text)",
				"scheduler_private.reconcile_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,uuid)",
			} {
				for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_controller", "devpilot_scheduler_supervisor", "devpilot_scheduler_api"} {
					var yes bool
					if e := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, sig).Scan(&yes); e != nil || yes != (role == "devpilot_scheduler_worker") {
						t.Fatal("new exact ACL", role, yes, e)
					}
				}
				var raw []byte
				if e := db.QueryRow(ctx, `SELECT jsonb_build_object('signature',oid::regprocedure::text,'owner',pg_get_userbyid(proowner),'acl',proacl,'settings',proconfig) FROM pg_proc WHERE oid=$1::regprocedure`, sig).Scan(&raw); e != nil {
					t.Fatal(e)
				}
				t.Log(string(raw))
			}
		}
		before := minimalLedger(t, db)
		if mode == "validation" {
			a := commitArgs(s, l, r)
			for i := 0; i < 11; i++ {
				b := append([]any(nil), a...)
				want := "P0001"
				if i >= 5 && i <= 8 {
					b[i] = int64(999999)
				} else {
					b[i] = "00000000-0000-0000-0000-000000000001"
				}
				if i == 0 {
					want = "42501"
				}
				commitCall(t, s, b, false, want)
			}
			for _, idx := range []int{11, 12, 14} {
				b := append([]any(nil), a...)
				b[idx] = nil
				commitCall(t, s, b, false, "22023")
			}
			b := append([]any(nil), a...)
			b[15] = "raw_error_COOKIE_SECRET_RESULT_SENTINEL"
			commitCall(t, s, b, false, "22023")
			other, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
			b = append([]any(nil), a...)
			b[0] = inc
			commitCall(t, scheduler.Store{DB: other, Incarnation: inc}, b, false, "P0001")
			// Actual successful SQL rolled back: no result or projection is left.
			commitCall(t, s, a, false, "00000")
			if minimalLedger(t, db) != before {
				t.Fatal("rollback changed ledger")
			}
			if found, e := s.ReconcileResult(ctx, c); e != nil || found {
				t.Fatal("rollback reconciliation", found, e)
			}
			// Exact raw ID replay succeeds, but a conflicting immutable payload refuses.
			commitCall(t, s, a, true, "00000")
			commitCall(t, s, a, true, "00000")
			b = append([]any(nil), a...)
			b[12] = int(r.Duration/time.Millisecond) + 1
			commitCall(t, s, b, false, "P0001")
		} else {
			if found, e := s.ReconcileResult(ctx, c); e != nil || found {
				t.Fatal("precommit snapshot", found, e)
			}
			if mode == "rollback" {
				lost := s
				lost.DB = workspaceObservedDB{Pool: s.DB.(*pgxpool.Pool), wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "rollback"} }}
				if e := lost.CommitResult(ctx, c); !errors.Is(e, scheduler.ErrCommitUnknown) {
					t.Fatal("actual rollback still unknown to caller", e)
				}
				if found, e := s.ReconcileResult(ctx, c); e != nil || found {
					t.Fatal("rollback snapshot", found, e)
				}
				if minimalLedger(t, db) != before {
					t.Fatal("rollback leaked projection/resources")
				}
				if e := s.CommitResult(ctx, c); e != nil {
					t.Fatal("same-ID retry after rollback", e)
				}
			} else if mode == "ack" {
				lost := s
				lost.DB = workspaceObservedDB{Pool: s.DB.(*pgxpool.Pool), wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
				if e := lost.CommitResult(ctx, c); !errors.Is(e, scheduler.ErrCommitUnknown) {
					t.Fatal("real commit ACK loss", e)
				}
			} else if mode == "race" || mode == "authority" {
				pool2 := s.DB.(*pgxpool.Pool)
				inc2 := s.Incarnation
				if mode == "authority" {
					pool2, inc2 = schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
				}
				barrier, e := db.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer barrier.Rollback(ctx)
				if _, e = barrier.Exec(ctx, `SELECT 1 FROM workspaces WHERE id=$1 FOR UPDATE`, l.WorkspaceID); e != nil {
					t.Fatal(e)
				}
				done := make(chan error, 2)
				pids := []int{}
				for i := 0; i < 2; i++ {
					pool := s.DB.(*pgxpool.Pool)
					inc := s.Incarnation
					if i == 1 {
						pool = pool2
						inc = inc2
					}
					conn, e := pool.Acquire(ctx)
					if e != nil {
						t.Fatal(e)
					}
					pid := int(conn.Conn().PgConn().PID())
					pids = append(pids, pid)
					worker := s
					worker.DB = conn
					worker.Incarnation = inc
					go func() { e := func() error { defer conn.Release(); return worker.CommitResult(ctx, c) }(); done <- e }()
					waitWorkspaceBlock(t, db, pid)
				}
				locks := schedulerLockSnapshot(t, db, "same-result-id-two-real-connections", pids...)
				if e = barrier.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				t.Log(string(locks))
				refused := 0
				for i := 0; i < 2; i++ {
					e := <-done
					if errors.Is(e, scheduler.ErrCandidate) {
						refused++
					} else if e != nil {
						t.Fatal(e)
					}
				}
				expected := 0
				if mode == "authority" {
					expected = 1
				}
				if refused != expected {
					t.Fatal("foreign Worker race", refused, expected)
				}
			} else if e := s.CommitResult(ctx, c); e != nil {
				t.Fatal(e)
			}
			stable := minimalLedger(t, db)
			for i := 0; i < 2; i++ {
				if found, e := s.ReconcileResult(ctx, c); e != nil || !found {
					t.Fatal("committed reconciliation", found, e)
				}
				if e := s.CommitResult(ctx, c); e != nil {
					t.Fatal("idempotent retry", e)
				}
			}
			if minimalLedger(t, db) != stable {
				t.Fatal("replay/readback changed projection/resource ledger")
			}
			second, e := scheduler.NewResultCommit(v)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.CommitResult(ctx, second); !errors.Is(e, scheduler.ErrCandidate) {
				t.Fatal("different ID", e)
			}
		}
		want := 1
		if mode == "patch" || mode == "pause" || mode == "renew" {
			want = 0
		}
		if got := queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND latest_result_run_id=$2`, l.MonitorID, l.RunID); got != want {
			t.Fatal("current projection", got, want)
		}
		if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='completed' AND configuration_version=$2 AND activation_generation=$3 AND scheduler_epoch=$4 AND physical_release_confirmed_at IS NOT NULL`, l.RunID, l.ConfigurationVersion, l.ActivationGeneration, l.SchedulerEpoch) != 1 {
			t.Fatal("original result audit")
		}
		minimalObserve(t, db, l)
	}
	TestBudgetJointKernelPG(t)
}
func TestResultCommitValidationPG(t *testing.T) { runCommit(t, "validation") }
func TestResultCommitACKPG(t *testing.T)        { runCommit(t, "ack") }
func TestResultCommitRacePG(t *testing.T)       { runCommit(t, "race") }
func TestResultCommitPatchPG(t *testing.T)      { runCommit(t, "patch") }
func TestResultCommitPausePG(t *testing.T)      { runCommit(t, "pause") }
func TestResultCommitRenewPG(t *testing.T)      { runCommit(t, "renew") }

func TestResultCommitRollbackPG(t *testing.T)  { runCommit(t, "rollback") }
func TestResultCommitAuthorityPG(t *testing.T) { runCommit(t, "authority") }
