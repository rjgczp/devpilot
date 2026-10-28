//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

const minimalSQL = `SELECT scheduler_private.persist_minimal_result($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::text,$13::integer,$14::integer,$15::boolean,$16::text,$17::text)`

func minimalArgs(s scheduler.Store, l handoff.Lineage, r *monitoring.Result) []any {
	var ns, sup, ds, status, success, failure any
	if l.NamespaceIncarnation != "" {
		ns = l.NamespaceIncarnation
	}
	if l.SupervisorIncarnation != "" {
		sup = l.SupervisorIncarnation
	}
	if r != nil {
		ds = int(r.Duration / time.Millisecond)
		status = r.HTTPStatus
		success = r.Success
		if r.FailureCode != nil {
			failure = string(*r.FailureCode)
		}
	}
	return []any{s.Incarnation, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.ConfigurationVersion, l.ActivationGeneration, l.SchedulerEpoch, l.FencingToken, ns, sup, "network", ds, status, success, failure, nil}
}
func minimalCall(t *testing.T, s scheduler.Store, a []any, commit bool, want string) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var raw []byte
	e = tx.QueryRow(ctx, minimalSQL, a...).Scan(&raw)
	budgetSQLState(t, e, want)
	if e == nil {
		if len(raw) > 256 || strings.Contains(string(raw), "result_commit_id") {
			t.Fatal("unbounded or retry projection")
		}
		if commit {
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
		}
	}
}

// The goroutine owns its acquired connection through rollback and release.
// A result notification is sent only after those deferred operations return.
// This avoids the initial race where the parent returned a still-used Conn.
func minimalAsync(conn *pgxpool.Conn, args []any) <-chan error {
	done := make(chan error, 1)
	go func() {
		e := func() error {
			defer conn.Release()
			ctx := context.Background()
			tx, e := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if e != nil {
				return e
			}
			defer tx.Rollback(ctx)
			var raw []byte
			return tx.QueryRow(ctx, minimalSQL, args...).Scan(&raw)
		}()
		done <- e
	}()
	return done
}
func minimalLedger(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var raw []byte
	if e := db.QueryRow(context.Background(), `SELECT jsonb_build_object('budgets',(SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b),'reservations',(SELECT jsonb_agg(to_jsonb(q) ORDER BY run_id) FROM scheduler_reservations q),'schedules',(SELECT jsonb_agg(to_jsonb(s) ORDER BY monitor_id) FROM monitor_schedules s),'activation',(SELECT jsonb_agg(to_jsonb(a) ORDER BY monitor_id) FROM monitor_activation_states a))`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func minimalObserve(t *testing.T, db *pgxpool.Pool, l handoff.Lineage) {
	t.Helper()
	var raw []byte
	if e := db.QueryRow(context.Background(), `SELECT jsonb_build_object('at',clock_timestamp(),'run',to_jsonb(r)) FROM monitor_runs r WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4`, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	t.Log("minimal-result witness", string(raw))
	for _, sentinel := range []string{"private-path-SENTINEL", "a.fixture.example.com", "Cookie", "password", "SECRET_RESULT_SENTINEL", "raw_error"} {
		if strings.Contains(string(raw), sentinel) {
			t.Fatal("sensitive result leak")
		}
	}
}
func runMinimal(t *testing.T, mode string) {
	if jointResultCheck != nil {
		t.Fatal("nested fixture")
	}
	defer func() { jointResultCheck = nil; jointResultPatch = nil }()
	jointResultCheck = func(t *testing.T, stage string, db *pgxpool.Pool, s scheduler.Store, l handoff.Lineage, r *monitoring.Result) {
		if stage == "claimed" && mode == "patch" {
			jointResultPatch = func(t *testing.T, f memberFixture, m monitor.Public) {
				name := "updated-after-physical-stop"
				updated, e := (monitor.Store{Pool: db}).Update(context.Background(), f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &name}, monitor.ETag(m))
				if e != nil || updated.Version == m.Version {
					t.Fatal("real Owner PATCH failed", e)
				}
			}
		}
		before := minimalLedger(t, db)
		defer func() {
			if minimalLedger(t, db) != before {
				t.Fatal("result modified physical/rate/cooling/schedule/activation ledger")
			}
		}()
		args := minimalArgs(s, l, r)
		if stage == "claimed" {
			t.Run("preflight-control-rollback", func(t *testing.T) {
				a := append([]any(nil), args...)
				a[11] = "preflight"
				a[16] = "invalid_configuration"
				minimalCall(t, s, a, false, "00000")
			})
			t.Run("grant-and-secret-boundary", func(t *testing.T) {
				const sig = "scheduler_private.persist_minimal_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text,integer,integer,boolean,text,text)"
				for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_controller", "devpilot_scheduler_supervisor", "devpilot_scheduler_api"} {
					var yes bool
					if e := db.QueryRow(context.Background(), `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, sig).Scan(&yes); e != nil || yes != (role == "devpilot_scheduler_worker") {
						t.Fatal(role, yes, e)
					}
				}
				var raw []byte
				if e := db.QueryRow(context.Background(), `SELECT jsonb_build_object('signature',oid::regprocedure::text,'owner',pg_get_userbyid(proowner),'definer',prosecdef,'volatile',provolatile,'settings',proconfig,'acl',proacl) FROM pg_proc WHERE oid=$1::regprocedure`, sig).Scan(&raw); e != nil {
					t.Fatal(e)
				}
				t.Log(string(raw))
				for _, sql := range []string{`SELECT password_hash FROM users LIMIT 1`, `SELECT token_hash FROM sessions LIMIT 1`, `UPDATE monitor_runs SET success=true`, `UPDATE scheduler_reservations SET reservation_state='released'`} {
					_, e := s.DB.(*pgxpool.Pool).Exec(context.Background(), sql)
					budgetSQLState(t, e, "42501")
				}
			})
			return
		}
		if stage == "before-stop" {
			if r != nil {
				t.Run("network-before-physical-proof-rejected", func(t *testing.T) { minimalCall(t, s, args, false, "P0001") })
			}
			return
		}
		if stage == "unconfirmed" {
			t.Run("stop-unconfirmed-is-not-http-result-or-release", func(t *testing.T) {
				v, e := scheduler.ControlResult(l, "physical_stop_unconfirmed", "worker_lost")
				if e != nil {
					t.Fatal(e)
				}
				if e = s.PersistMinimalResult(context.Background(), v); e != nil {
					t.Fatal(e)
				}
				if e = s.PersistMinimalResult(context.Background(), v); !errors.Is(e, scheduler.ErrResult) {
					t.Fatal("duplicate capability", e)
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='started' AND stop_reason='worker_lost' AND finished_at IS NULL AND success IS NULL AND result_commit_id IS NULL AND physical_release_confirmed_at IS NULL`, l.RunID) != 1 {
					t.Fatal("invented HTTP result")
				}
				minimalObserve(t, db, l)
			})
			return
		}
		if stage != "released" {
			t.Fatal("unexpected fixture stage")
		}
		if r == nil {
			t.Run("permit-denial-control-only-single-assignment", func(t *testing.T) {
				v, e := scheduler.ControlResult(l, "permit_denied", "egress_unavailable")
				if e != nil {
					t.Fatal(e)
				}
				if e = s.PersistMinimalResult(context.Background(), v); e != nil {
					t.Fatal(e)
				}
				a := minimalArgs(s, l, nil)
				a[11] = "permit_denied"
				a[16] = "commit_unknown"
				minimalCall(t, s, a, false, "P0001")
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND stop_reason='egress_unavailable' AND attempt_state='started' AND finished_at IS NULL AND http_status IS NULL AND success IS NULL AND failure_code IS NULL AND result_commit_id IS NULL`, l.RunID) != 1 {
					t.Fatal("control refusal fabricated HTTP result")
				}
				minimalObserve(t, db, l)
			})
			return
		}
		if mode == "expiry" {
			var until time.Time
			if e := db.QueryRow(context.Background(), `SELECT lease_until FROM monitor_runs WHERE run_id=$1`, l.RunID).Scan(&until); e != nil {
				t.Fatal(e)
			}
			t.Log("actual Lease expiry wait", until)
			time.Sleep(time.Until(until) - 100*time.Millisecond)
			t.Run("expiry-during-final-control-lock-wait", func(t *testing.T) {
				ctx := context.Background()
				barrier, e := db.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer barrier.Rollback(ctx)
				if _, e = barrier.Exec(ctx, `SELECT 1 FROM scheduler_control WHERE incarnation=$1 FOR UPDATE`, s.Incarnation); e != nil {
					t.Fatal(e)
				}
				conn, e := s.DB.(*pgxpool.Pool).Acquire(ctx)
				if e != nil {
					t.Fatal(e)
				}
				pid := int(conn.Conn().PgConn().PID())
				done := minimalAsync(conn, args)
				waitWorkspaceBlock(t, db, pid)
				schedulerLockReceipt(t, db, "result-final-control-wait-crosses-real-lease-expiry", pid)
				time.Sleep(time.Until(until) + 20*time.Millisecond)
				if e = barrier.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				budgetSQLState(t, <-done, "P0001")
			})
			minimalObserve(t, db, l)
			return
		}
		if mode == "clock" {
			t.Run("stored-finish-time-uses-durable-clock-floor", func(t *testing.T) {
				var floor time.Time
				if e := db.QueryRow(context.Background(), `UPDATE scheduler_control SET last_gate_at=clock_timestamp()+interval '1 second' WHERE incarnation=$1 RETURNING last_gate_at`, s.Incarnation).Scan(&floor); e != nil {
					t.Fatal(e)
				}
				v, e := scheduler.NetworkResult(l, r)
				if e != nil {
					t.Fatal(e)
				}
				if e = s.PersistMinimalResult(context.Background(), v); e != nil {
					t.Fatal(e)
				}
				var finish time.Time
				if e = db.QueryRow(context.Background(), `SELECT finished_at FROM monitor_runs WHERE run_id=$1`, l.RunID).Scan(&finish); e != nil || finish.Before(floor) {
					t.Fatal("result time moved behind durable marker", e)
				}
				t.Log("durable clock lower bound", floor, "stored finish", finish)
				minimalObserve(t, db, l)
			})
			return
		}
		if mode == "terminal" {
			// Fixture state construction only, after real accepted physical proof. It
			// does not implement recovery or pretend an admin UPDATE is stop evidence.
			if _, e := db.Exec(context.Background(), `UPDATE monitor_runs SET attempt_state='unknown',finished_at=clock_timestamp() WHERE run_id=$1`, l.RunID); e != nil {
				t.Fatal(e)
			}
			t.Run("late-result-cannot-rewrite-unknown", func(t *testing.T) { minimalCall(t, s, args, false, "P0001"); minimalObserve(t, db, l) })
			return
		}
		if mode == "control-race" {
			t.Run("control-and-http-report-race-one-winner", func(t *testing.T) {
				// Competing trusted reports test single assignment; this does not assert
				// that the successful receiver transaction was actually a permit refusal.
				start := make(chan struct{})
				done := make(chan error, 2)
				go func() {
					<-start
					v, e := scheduler.NetworkResult(l, r)
					if e == nil {
						e = s.PersistMinimalResult(context.Background(), v)
					}
					done <- e
				}()
				go func() {
					<-start
					v, e := scheduler.ControlResult(l, "permit_denied", "egress_unavailable")
					if e == nil {
						e = s.PersistMinimalResult(context.Background(), v)
					}
					done <- e
				}()
				close(start)
				wins := 0
				for i := 0; i < 2; i++ {
					e := <-done
					if e == nil {
						wins++
					} else if !errors.Is(e, scheduler.ErrCandidate) {
						t.Fatal(e)
					}
				}
				if wins != 1 {
					t.Fatal("report race winners", wins)
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND ((attempt_state='completed' AND stop_reason IS NULL AND success IS NOT NULL) OR (attempt_state='started' AND stop_reason='egress_unavailable' AND success IS NULL AND result_commit_id IS NULL))`, l.RunID) != 1 {
					t.Fatal("mixed control and HTTP outcome")
				}
				minimalCall(t, s, args, false, "P0001")
				minimalObserve(t, db, l)
			})
			return
		}
		if mode == "unknown" {
			t.Run("actual-result-commit-lost-ack-no-resend", func(t *testing.T) {
				v, e := scheduler.NetworkResult(l, r)
				if e != nil {
					t.Fatal(e)
				}
				lost := s
				lost.DB = workspaceObservedDB{Pool: s.DB.(*pgxpool.Pool), wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
				if e = lost.PersistMinimalResult(context.Background(), v); !errors.Is(e, scheduler.ErrCommitUnknown) {
					t.Fatal(e)
				}
				if e = s.PersistMinimalResult(context.Background(), v); !errors.Is(e, scheduler.ErrResult) {
					t.Fatal("unknown commit retried", e)
				}
				minimalCall(t, s, args, false, "P0001")
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='completed'`, l.RunID) != 1 {
					t.Fatal("actual commit missing")
				}
				minimalObserve(t, db, l)
			})
			return
		}
		t.Run("invalid-lineage-and-payload-atomic-rejection", func(t *testing.T) {
			for _, i := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
				a := append([]any(nil), args...)
				if i >= 5 && i <= 8 {
					a[i] = "9223372036854775807"
				} else {
					a[i] = "00000000-0000-0000-0000-000000000001"
				}
				want := "P0001"
				if i == 0 {
					want = "42501"
				}
				minimalCall(t, s, a, false, want)
			}
			for _, c := range []struct {
				i int
				v any
			}{{11, "SECRET_RESULT_SENTINEL"}, {12, -1}, {12, 30001}, {12, nil}, {13, 0}, {13, 600}, {14, nil}, {15, "raw_error_SECRET_RESULT_SENTINEL"}, {16, "Cookie=SECRET_RESULT_SENTINEL"}} {
				a := append([]any(nil), args...)
				a[c.i] = c.v
				minimalCall(t, s, a, false, "22023")
			}
		})
		t.Run("durable-clock-floor-rejects-backward-time", func(t *testing.T) {
			// Only the private fixture's existing Worker clock marker is varied. This
			// neither alters the host clock, Lease, physical proof nor any resource.
			ctx := context.Background()
			var saved time.Time
			if e := db.QueryRow(ctx, `SELECT last_gate_at FROM scheduler_control WHERE incarnation=$1`, s.Incarnation).Scan(&saved); e != nil {
				t.Fatal(e)
			}
			for _, span := range []string{"1 second", "10 seconds"} {
				if _, e := db.Exec(ctx, `UPDATE scheduler_control SET last_gate_at=clock_timestamp()+$2::interval WHERE incarnation=$1`, s.Incarnation, span); e != nil {
					t.Fatal(e)
				}
				tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if e != nil {
					t.Fatal(e)
				}
				var raw []byte
				e = tx.QueryRow(ctx, minimalSQL, args...).Scan(&raw)
				if span == "10 seconds" {
					var pe *pgconn.PgError
					if !errors.As(e, &pe) || pe.Code != "P0001" || pe.Message != "clock_untrusted" {
						t.Fatal("clock gate", e)
					}
				} else {
					budgetSQLState(t, e, "00000")

				}
				if e = tx.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := db.Exec(ctx, `UPDATE scheduler_control SET last_gate_at=$2 WHERE incarnation=$1`, s.Incarnation, saved); e != nil {
				t.Fatal(e)
			}
		})
		t.Run("all-original-fixed-failure-codes-nullability-rollback", func(t *testing.T) {
			for _, code := range []string{"dns_failed", "blocked_target", "connect_failed", "tls_failed", "timeout", "canceled", "response_limit", "protocol_error", "unexpected_status"} {
				a := append([]any(nil), args...)
				a[14] = false
				a[15] = code
				a[13] = nil
				if code == "unexpected_status" {
					a[13] = 503
				}
				minimalCall(t, s, a, false, "00000")
			}
		})
		t.Run("wrong-worker-cannot-report-owned-run", func(t *testing.T) {
			other, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
			a := append([]any(nil), args...)
			a[0] = inc
			minimalCall(t, scheduler.Store{DB: other, Incarnation: inc}, a, false, "P0001")
		})
		t.Run("real-parent-wait-locks-and-control-rollback", func(t *testing.T) {
			ctx := context.Background()
			pool := s.DB.(*pgxpool.Pool)
			conn, e := pool.Acquire(ctx)
			if e != nil {
				t.Fatal(e)
			}
			pid := int(conn.Conn().PgConn().PID())
			barrier, e := db.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer barrier.Rollback(ctx)
			if _, e = barrier.Exec(ctx, `SELECT 1 FROM monitor_schedules WHERE monitor_id=$1 FOR UPDATE`, l.MonitorID); e != nil {
				t.Fatal(e)
			}
			done := minimalAsync(conn, args)
			waitWorkspaceBlock(t, db, pid)
			schedulerLockReceipt(t, db, "result-wait-after-parents", pid)
			if e = barrier.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			budgetSQLState(t, <-done, "00000")
			// Validate a control write and roll back its data and identity stamp.
			// The separate control/result race uses actual independent commits.
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			a := append([]any(nil), args...)
			a[11] = "permit_denied"
			a[12] = nil
			a[13] = nil
			a[14] = nil
			a[15] = nil
			a[16] = "egress_unavailable"
			var raw []byte
			e = tx.QueryRow(ctx, minimalSQL, a...).Scan(&raw)
			budgetSQLState(t, e, "00000")
			if e = tx.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
		})
		t.Run("two-independent-connections-only-one-result", func(t *testing.T) {
			// Both use the Run's authenticated Worker identity, not a foreign Worker.
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			start := make(chan struct{})
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					v, e := scheduler.NetworkResult(l, r)
					if e == nil {
						e = s.PersistMinimalResult(context.Background(), v)
					}
					errs <- e
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			wins := 0
			for e := range errs {
				if e == nil {
					wins++
				} else if !errors.Is(e, scheduler.ErrCandidate) {
					t.Fatal(e)
				}
			}
			if wins != 1 {
				t.Fatal("result winners", wins)
			}
			minimalCall(t, s, args, false, "P0001")
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='completed' AND result_commit_id IS NOT NULL AND finished_at IS NOT NULL`, l.RunID) != 1 {
				t.Fatal("missing minimal result")
			}
			minimalObserve(t, db, l)
			var payload []byte
			if e := db.QueryRow(context.Background(), `SELECT to_jsonb(r) FROM monitor_runs r WHERE run_id=$1`, l.RunID).Scan(&payload); e != nil {
				t.Fatal(e)
			}
			var fields map[string]any
			if json.Unmarshal(payload, &fields) != nil {
				t.Fatal("invalid final record")
			}
			if fields["success"] != r.Success {
				t.Fatal("changed kernel outcome")
			}
		})
	}
	TestBudgetJointKernelPG(t)
	t.Log(fmt.Sprintf("task7.1 mode=%s; existing joint assertions retained", mode))
}
func TestResultMinimalPG(t *testing.T)           { runMinimal(t, "normal") }
func TestResultUnknownCommitPG(t *testing.T)     { runMinimal(t, "unknown") }
func TestResultActualLeaseExpiryPG(t *testing.T) { runMinimal(t, "expiry") }

func TestResultAfterOwnerPatchPG(t *testing.T) { runMinimal(t, "patch") }

func TestResultTerminalUnknownPG(t *testing.T) { runMinimal(t, "terminal") }
func TestResultControlRacePG(t *testing.T)     { runMinimal(t, "control-race") }
func TestResultPreflightPG(t *testing.T) {
	db := budgetDB(t)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	f, m := monitorFixture(t, db)
	h := budgetHint(t, db, f, m)
	store := scheduler.Store{DB: w, Incarnation: inc}
	lease, e := store.Claim(context.Background(), checkpointTwoHint(h))
	if e != nil {
		t.Fatal(e)
	}
	l := handoff.Lineage{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m, RunID: h.r, ConfigurationVersion: h.v, ActivationGeneration: h.g, SchedulerEpoch: h.e, FencingToken: lease.FencingToken}
	before := minimalLedger(t, db)
	v, e := scheduler.ControlResult(l, "preflight", "invalid_configuration")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.PersistMinimalResult(context.Background(), v); e != nil {
		t.Fatal(e)
	}
	a := minimalArgs(store, l, nil)
	a[11] = "preflight"
	a[16] = "resource_limited"
	minimalCall(t, store, a, false, "P0001")
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='leased' AND stop_reason='invalid_configuration' AND started_at IS NULL AND finished_at IS NULL AND success IS NULL AND result_commit_id IS NULL AND physical_release_confirmed_at IS NULL`, h.r) != 1 {
		t.Fatal("preflight fabricated execution/result")
	}
	if minimalLedger(t, db) != before {
		t.Fatal("preflight returned capacity or changed schedule")
	}
	minimalObserve(t, db, l)
}

func TestResultClockFloorPG(t *testing.T) { runMinimal(t, "clock") }
