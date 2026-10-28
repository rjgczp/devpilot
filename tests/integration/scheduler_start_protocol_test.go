//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

type startCommitGate struct {
	pgx.Tx
	pause *workspacePause
}

func (t startCommitGate) Commit(ctx context.Context) error {
	t.pause.stop(ctx)
	return t.Tx.Commit(ctx)
}

type startProtocolResult struct {
	snapshot scheduler.StartSnapshot
	err      error
}

func assertNoDBDuringInput(t *testing.T, db, w, s *pgxpool.Pool) {
	t.Helper()
	if w.Stat().AcquiredConns() != 0 || s.Stat().AcquiredConns() != 0 {
		t.Fatal("input retains a pool connection")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE usename IN ('scheduler_claim_worker1','scheduler_claim_worker2','scheduler_claim_supervisor') AND xact_start IS NOT NULL`) != 0 {
		t.Fatal("input retains database transaction")
	}
}
func TestBudgetStartCommitAcknowledgements(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	ctx := context.Background()
	for _, outcome := range []string{"success", "lost", "rollback"} {
		t.Run(outcome, func(t *testing.T) {
			_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
			store := scheduler.Store{DB: w, Incarnation: wi}
			lease, err := store.Claim(ctx, checkpointTwoHint(h))
			if err != nil {
				t.Fatal(err)
			}
			budgetPrepare(t, db, s, si, wi, h)
			start := scheduler.Store{DB: workspaceObservedDB{Pool: w, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: outcome} }}, Incarnation: wi}
			snapshot, err := start.Start(ctx, checkpointTwoHint(h), lease)
			if outcome != "success" {
				if !errors.Is(err, scheduler.ErrCommitUnknown) {
					t.Fatal("unknown Start accepted", err)
				}
				pipes, e := snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
				pipes.Close()
				if !errors.Is(e, scheduler.ErrInputConsumed) {
					t.Fatal("unconfirmed Start exposed input", e)
				}
				want := 0
				if outcome == "lost" {
					want = 1
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='started'`, h.r) != want {
					t.Fatal("real commit fault outcome")
				}
				if want == 1 {
					if _, e = store.Start(ctx, checkpointTwoHint(h), lease); !errors.Is(e, scheduler.ErrCandidate) {
						t.Fatal("readback restarted original run", e)
					}
				}
				if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND input_consumed_at IS NOT NULL`, h.r) != 0 {
					t.Fatal("unknown Start delivered")
				}
				t.Logf("actual Start commit=%s observed ErrCommitUnknown; no pipe/capability returned or restored", outcome)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			pipes, err := snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
			if err != nil {
				t.Fatal(err)
			}
			defer pipes.Close()
			assertNoDBDuringInput(t, db, w, s)
			expected, err := handoff.ReadReceipt(pipes.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			receiver := handoff.NewReceiver(expected)
			input, err := monitoring.ReceiveSchedulerInput(receiver, pipes.Envelope)
			if err != nil || !input.Valid() || input.Lineage().RunID != h.r {
				t.Fatal("real committed Start pipe lineage", err)
			}
			if _, err = receiver.Receive(pipes.Envelope); !errors.Is(err, handoff.ErrInput) {
				t.Fatal("receiver repeated capability")
			}
			if _, err = snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si}); !errors.Is(err, scheduler.ErrInputConsumed) {
				t.Fatal("snapshot copy reissued capability")
			}
			assertNoDBDuringInput(t, db, w, s)
			if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND input_consumed_at IS NOT NULL`, h.r) != 1 {
				t.Fatal("missing durable input consumption")
			}
			t.Log("real Start+input commits confirmed, both anonymous pipes consumed once; zero acquired connections/active transactions during offline bridge")
		})
	}
}
func TestBudgetInputCommitAcknowledgements(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	ctx := context.Background()
	for _, outcome := range []string{"lost", "rollback"} {
		t.Run(outcome, func(t *testing.T) {
			_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
			store := scheduler.Store{DB: w, Incarnation: wi}
			lease, err := store.Claim(ctx, checkpointTwoHint(h))
			if err != nil {
				t.Fatal(err)
			}
			budgetPrepare(t, db, s, si, wi, h)
			snapshot, err := store.Start(ctx, checkpointTwoHint(h), lease)
			if err != nil {
				t.Fatal(err)
			}
			uncertain := scheduler.InputController{DB: workspaceObservedDB{Pool: s, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: outcome} }}, Incarnation: si}
			pipes, err := snapshot.IssueInput(ctx, uncertain)
			defer pipes.Close()
			if !errors.Is(err, scheduler.ErrCommitUnknown) || pipes.Envelope != nil || pipes.Receipt != nil {
				t.Fatal("uncertain input ACK exposed pipes", err)
			}
			if _, err = snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si}); !errors.Is(err, scheduler.ErrInputConsumed) {
				t.Fatal("failed input was reconnected", err)
			}
			if _, err = store.Start(ctx, checkpointTwoHint(h), lease); !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("process restart restored snapshot", err)
			}
			want := 0
			if outcome == "lost" {
				want = 1
			}
			if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND input_consumed_at IS NOT NULL`, h.r) != want {
				t.Fatal("actual independent input commit mismatch")
			}
			assertNoDBDuringInput(t, db, w, s)
			t.Logf("real input commit=%s uncertain -> zero pipes, sender burned, no same-run restart", outcome)
		})
	}
}
func TestBudgetStartPausePatchCommitOrders(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, wi2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	sup, si := schedulerSupervisorLogin(t, db)
	ctx := context.Background()
	for _, mutation := range []string{"Pause", "PATCH"} {
		for _, order := range []string{"mutation-first", "Start-first"} {
			t.Run(mutation+"/"+order, func(t *testing.T) {
				f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
				h2 := checkpointTwoPromote(t, db, f, h)
				store := scheduler.Store{DB: w, Incarnation: wi}
				lease, err := store.Claim(ctx, checkpointTwoHint(h))
				if err != nil {
					t.Fatal(err)
				}
				budgetPrepare(t, db, sup, si, wi, h)
				a := activationRead(t, db, f.md, m)
				gate := newWorkspacePause(t)
				defer gate.resume()
				actorPID := make(chan int, 1)
				workerPID := make(chan int, 1)
				actorDone := make(chan error, 1)
				workerDone := make(chan startProtocolResult, 1)
				mutate := func(block bool) {
					var pool monitor.Database = workspaceObservedDB{Pool: db, pid: actorPID}
					if block {
						pool = workspaceObservedDB{Pool: db, pid: actorPID, wrap: func(tx pgx.Tx) pgx.Tx { return startCommitGate{tx, gate} }}
					}
					var e error
					if mutation == "Pause" {
						_, e = (monitor.Store{Pool: pool}).ActivationUpdate(ctx, f.md, h.w, h.p, h.m, monitor.ActivationInput{DesiredState: "disabled", ConfigurationVersion: h.v}, monitor.ActivationETag(a))
					} else {
						_, e = (monitor.Store{Pool: pool}).Update(ctx, f.md, h.w, h.p, h.m, monitor.Input{Name: stringPointer("new frozen configuration"), URL: stringPointer("https://after-patch.example.com/POST_PATCH_SENTINEL")}, monitor.ETag(m))
					}
					actorDone <- e
				}
				start := func(block bool) {
					var database scheduler.Database = workspaceObservedDB{Pool: w, pid: workerPID}
					if block {
						database = workspaceObservedDB{Pool: w, pid: workerPID, wrap: func(tx pgx.Tx) pgx.Tx { return startCommitGate{tx, gate} }}
					}
					snapshot, e := (scheduler.Store{DB: database, Incarnation: wi}).Start(ctx, checkpointTwoHint(h), lease)
					workerDone <- startProtocolResult{snapshot, e}
				}
				var ap, wp int
				if order == "mutation-first" {
					go mutate(true)
					ap = <-actorPID
					gate.wait(t)
					go start(false)
					wp = <-workerPID
					waitActivationBlock(t, db, wp)
				} else {
					go start(true)
					wp = <-workerPID
					gate.wait(t)
					if len(workerDone) != 0 {
						t.Fatal("snapshot escaped before Commit ACK")
					}
					go mutate(false)
					ap = <-actorPID
					waitActivationBlock(t, db, ap)
					_, err = schedulerClaim(t, w2, wi2, h2)
					budgetSQLState(t, err, "P0001")
				}
				schedulerLockReceipt(t, db, mutation+" "+order+" before Commit barrier release", ap, wp)
				gate.resume()
				if err = <-actorDone; err != nil {
					t.Fatal(err)
				}
				result := <-workerDone
				if order == "mutation-first" {
					if !errors.Is(result.err, scheduler.ErrCandidate) {
						t.Fatal("Start ignored committed mutation", result.err)
					}
					if _, err = result.snapshot.IssueInput(ctx, scheduler.InputController{DB: sup, Incarnation: si}); !errors.Is(err, scheduler.ErrInputConsumed) {
						t.Fatal("failed Start exposed pipe")
					}
				} else {
					if result.err != nil {
						t.Fatal(result.err)
					}
					if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='started' AND configuration_version=1`, h.r) != 1 {
						t.Fatal("Start-first lineage lost")
					}
					pipes, e := result.snapshot.IssueInput(ctx, scheduler.InputController{DB: sup, Incarnation: si})
					if e != nil {
						t.Fatal(e)
					}
					defer pipes.Close()
					expected, e := handoff.ReadReceipt(pipes.Receipt)
					if e != nil {
						t.Fatal(e)
					}
					wire, e := handoff.NewReceiver(expected).Receive(pipes.Envelope)
					if e != nil || wire.ConfigurationVersion != h.v || wire.ActivationGeneration != h.g || string(wire.Target) != "https://example.com/private-path" {
						t.Fatal("old confirmed snapshot was replaced by current configuration", e)
					}
					assertNoDBDuringInput(t, db, w, sup)
					t.Log("Start-first snapshot retained original v/g/URL through mutation; in-flight semantics, input-only, no target network")
				}
				want := 0
				if order == "Start-first" {
					want = 1
				}
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND started_at IS NOT NULL`, h.r) != want {
					t.Fatal("commit order state")
				}
				checkpointTwoFinal(t, db, h)
			})
		}
	}
}
func TestBudgetStartLockStages(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	ctx := context.Background()
	stages := []struct{ name, sql, key, next string }{{"User", `SELECT id FROM users WHERE id=$1 FOR UPDATE`, "u", "workspaces"}, {"Workspace", `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, "w", "workspace_members"}, {"Membership", `SELECT user_id FROM workspace_members WHERE workspace_id=$1 FOR UPDATE`, "w", "projects"}, {"Project", `SELECT id FROM projects WHERE id=$1 FOR UPDATE`, "p", "http_monitors"}, {"Monitor", `SELECT id FROM http_monitors WHERE id=$1 FOR UPDATE`, "m", "monitor_activation_states"}, {"Activation", `SELECT monitor_id FROM monitor_activation_states WHERE monitor_id=$1 FOR UPDATE`, "m", "monitor_schedules"}, {"Schedule", `SELECT monitor_id FROM monitor_schedules WHERE monitor_id=$1 FOR UPDATE`, "m", "monitor_runs"}, {"Run", `SELECT run_id FROM monitor_runs WHERE run_id=$1 FOR UPDATE`, "r", "scheduler_budget_state"}, {"Budgets", `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' AND budget_key=$1 FOR UPDATE`, "global", "scheduler_reservations"}, {"Reservation", `SELECT run_id FROM scheduler_reservations WHERE run_id=$1 FOR UPDATE`, "r", ""}, {"Control", `SELECT control_id FROM scheduler_control WHERE control_id=$1 FOR UPDATE`, "deployment", ""}}
	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
			if _, err := schedulerClaim(t, w, wi, h); err != nil {
				t.Fatal(err)
			}
			budgetPrepare(t, db, s, si, wi, h)
			barrier, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer barrier.Rollback(ctx)
			key := map[string]string{"u": h.u, "w": h.w, "p": h.p, "m": h.m, "r": h.r, "global": "global", "deployment": "deployment"}[stage.key]
			if _, err = barrier.Exec(ctx, stage.sql, key); err != nil {
				t.Fatal(err)
			}
			conn, err := w.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			pid := int(conn.Conn().PgConn().PID())
			done := make(chan error, 1)
			go func() {
				c, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				tx, e := conn.BeginTx(c, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
				if e == nil {
					var raw []byte
					e = tx.QueryRow(c, schedulerStartSQL, wi, h.w, h.p, h.m, h.u, h.b, h.r, "1").Scan(&raw)
					_ = tx.Rollback(context.Background())
				}
				conn.Release()
				done <- e
			}()
			waitActivationBlock(t, db, pid)
			if stage.next != "" && queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND relation=$2::regclass AND mode='RowShareLock'`, pid, stage.next) != 0 {
				t.Fatal("Start acquired child before parent", stage.name)
			}
			schedulerLockReceipt(t, db, "Start blocked at "+stage.name, pid)
			if err = barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			budgetSQLState(t, <-done, "00000")
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='leased'`, h.r) != 1 {
				t.Fatal("inspection transaction did not roll back")
			}
		})
	}
}

func stringPointer(v string) *string { return &v }

func TestBudgetStartFinalExpiryAndStop(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	ctx := context.Background()
	for _, mode := range []string{"authorization-expiry", "emergency-stop", "owner-disabled", "supervisor-expiry"} {
		t.Run(mode, func(t *testing.T) {
			_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
			var expiry time.Time
			if mode == "authorization-expiry" {
				if err := db.QueryRow(ctx, `UPDATE monitor_schedules SET expires_at=clock_timestamp()+interval '200ms',scheduler_epoch=scheduler_epoch+1 WHERE monitor_id=$1 RETURNING expires_at,scheduler_epoch::text`, h.m).Scan(&expiry, &h.e); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := schedulerClaim(t, w, wi, h); err != nil {
				t.Fatal(err)
			}
			budgetPrepare(t, db, s, si, wi, h)
			barrier, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer barrier.Rollback(ctx)
			switch mode {
			case "authorization-expiry":
				_, err = barrier.Exec(ctx, `SELECT control_id FROM scheduler_control WHERE control_id='deployment' FOR UPDATE`)
			case "emergency-stop":
				_, err = barrier.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`)
			case "owner-disabled":
				_, err = barrier.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, h.u)
			case "supervisor-expiry":
				err = barrier.QueryRow(ctx, `UPDATE scheduler_control SET expires_at=clock_timestamp()+interval '100ms' WHERE kind='supervisor' AND incarnation=$1 RETURNING expires_at`, si).Scan(&expiry)
			}
			if err != nil {
				t.Fatal(err)
			}
			pid, ch := checkpointStartAsync(t, w, wi, h)
			waitActivationBlock(t, db, pid)
			schedulerLockReceipt(t, db, "Start final gate "+mode, pid)
			if !expiry.IsZero() {
				waitWorkspaceTime(t, db, expiry)
			}
			if err = barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			checkpointInvalidCandidate(t, <-ch)
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='leased' AND started_at IS NULL`, h.r) != 1 || queryInt(t, db, `SELECT tokens FROM scheduler_budget_state WHERE kind='project' AND budget_key=$1`, h.w+"/"+h.p) != 1000000 {
				t.Fatal("failed fresh gate consumed tokens/started")
			}
			if mode == "emergency-stop" {
				if _, err = db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false WHERE control_id='deployment'`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "supervisor-expiry" {
				if _, err = db.Exec(ctx, `UPDATE scheduler_control SET expires_at=clock_timestamp()+interval '1 hour' WHERE kind='supervisor' AND incarnation=$1`, si); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestBudgetStartRealLeaseHorizon(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	_, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
	ctx := context.Background()
	store := scheduler.Store{DB: w, Incarnation: wi}
	lease, err := store.Claim(ctx, checkpointTwoHint(h))
	if err != nil {
		t.Fatal(err)
	}
	// Wait to the original45s lease's timeout10+5s horizon. Never shorten the
	// durable lease with an administrator UPDATE, never extend any test timeout.
	var remaining float64
	if err = db.QueryRow(ctx, `SELECT extract(epoch FROM lease_until-interval '15 seconds'-clock_timestamp())::float8 FROM monitor_runs WHERE run_id=$1`, h.r).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining < 0 || remaining > 31 {
		t.Fatal("unexpected real45s lease horizon", remaining)
	}
	time.Sleep(time.Duration(remaining*float64(time.Second)) + time.Millisecond)
	budgetPrepare(t, db, s, si, wi, h)
	snapshot, err := store.Start(ctx, checkpointTwoHint(h), lease)
	if !errors.Is(err, scheduler.ErrCandidate) {
		t.Fatal("insufficient lease horizon admitted", err)
	}
	if _, err = snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si}); !errors.Is(err, scheduler.ErrInputConsumed) {
		t.Fatal("failed lease exposed input", err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='leased' AND started_at IS NULL AND physical_release_confirmed_at IS NULL`, h.r) != 1 {
		t.Fatal("lease refusal altered physical ledger")
	}
	t.Log("real original45s Lease: remaining <timeout10+5 rejected despite fresh35s preparation; no lease shortening or physical release")
}

func TestBudgetClaimPauseCommitOrders(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, wi2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	ctx := context.Background()
	for _, order := range []string{"Pause-first", "Claim-first"} {
		t.Run(order, func(t *testing.T) {
			f, m, h := schedulerClaimFixtureSeconds(t, db, 3600)
			h2 := checkpointTwoPromote(t, db, f, h)
			a := activationRead(t, db, f.md, m)
			gate := newWorkspacePause(t)
			defer gate.resume()
			pid := make(chan int, 1)
			paused := make(chan error, 1)
			pause := func(block bool) {
				var pool monitor.Database = workspaceObservedDB{Pool: db, pid: pid}
				if block {
					pool = workspaceObservedDB{Pool: db, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx { return startCommitGate{tx, gate} }}
				}
				_, err := (monitor.Store{Pool: pool}).ActivationUpdate(ctx, f.md, h.w, h.p, h.m, monitor.ActivationInput{DesiredState: "disabled", ConfigurationVersion: h.v}, monitor.ActivationETag(a))
				paused <- err
			}
			if order == "Pause-first" {
				go pause(true)
				pp := <-pid
				gate.wait(t)
				_, err := schedulerClaim(t, w, wi, h)
				budgetSQLState(t, err, "P0001")
				checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, h.u)
				schedulerLockReceipt(t, db, "Pause before Claim full skip rollback", pp)
				gate.resume()
				if err = <-paused; err != nil {
					t.Fatal(err)
				}
				_, err = schedulerClaim(t, w, wi, h)
				budgetSQLState(t, err, "P0001")
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1`, h.r) != 0 {
					t.Fatal("Pause-first created work")
				}
			} else {
				barrier, err := db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer barrier.Rollback(ctx)
				if _, err = barrier.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
					t.Fatal(err)
				}
				cp, claimed := startSchedulerClaim(t, w, wi, h)
				waitActivationBlock(t, db, cp)
				go pause(false)
				pp := <-pid
				waitActivationBlock(t, db, pp)
				_, err = schedulerClaim(t, w2, wi2, h2)
				budgetSQLState(t, err, "P0001")
				schedulerLockReceipt(t, db, "Claim holds U1/W; U2 Pause waiting; Worker2 skipped", cp, pp)
				if err = barrier.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				checkpointTwoOutcome(t, <-claimed, "00000")
				if err = <-paused; err != nil {
					t.Fatal(err)
				}
				_, err = schedulerStart(t, w, wi, h)
				budgetSQLState(t, err, "P0001")
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='leased' AND started_at IS NULL`, h.r) != 1 {
					t.Fatal("Pause after Claim authorized Start")
				}
			}
			checkpointTwoFinal(t, db, h)
		})
	}
}
