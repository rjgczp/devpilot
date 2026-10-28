//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture && recoveryfixture

package integration

import (
	"context"
	"encoding/json"
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

var recoverySetup func(*testing.T, *pgxpool.Pool, monitor.Public)
var recoveryPatch func(*testing.T, memberFixture, monitor.Public)
var recoveryCheck func(*testing.T, string, *pgxpool.Pool, scheduler.Store, handoff.Lineage, *monitoring.Result)
var recoveryCycles = 1
var recoveryCycle int
var recoveryExpected = "started"
var recoveryController scheduler.Store
var recoveryMode string
var recoveryExchange func(string, any) jointReply

const recoverSQL = `SELECT scheduler_private.recover_started($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::bigint,$10::uuid,$11::uuid,$12::text)`

func recoveryArgs(inc string, l handoff.Lineage, reason string) []any {
	return []any{inc, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.ConfigurationVersion, l.ActivationGeneration, l.SchedulerEpoch, l.FencingToken, l.NamespaceIncarnation, l.SupervisorIncarnation, reason}
}
func recoveryAssert(t *testing.T, db *pgxpool.Pool, l handoff.Lineage, state string, released bool) {
	t.Helper()
	var raw []byte
	if e := db.QueryRow(context.Background(), `SELECT to_jsonb(r) FROM monitor_runs r WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4`, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var r map[string]any
	if json.Unmarshal(raw, &r) != nil {
		t.Fatal("invalid Run")
	}
	if r["attempt_state"] != state || (r["physical_release_confirmed_at"] != nil) != released {
		t.Fatal("recovery fact", string(raw))
	}
	for _, k := range []string{"result_commit_id", "duration_ms", "http_status", "success", "failure_code"} {
		if r[k] != nil {
			t.Fatal("fabricated HTTP observation", k)
		}
	}
	t.Log("recovery minimal Run", string(raw))
}
func runRecovery(t *testing.T, mode string) {
	recoveryMode = mode
	recoveryCycles = 1
	if mode == "cycle" {
		recoveryCycles = 2
	}
	defer func() {
		recoveryCheck = nil
		recoveryPatch = nil
		recoverySetup = nil
		recoveryCycles = 1
		recoveryCycle = 0
		recoveryExpected = "started"
		recoveryMode = ""
		recoveryExchange = nil
		recoveryController = scheduler.Store{}
	}()
	var controller scheduler.Store
	var cp *pgxpool.Pool
	var firstLine handoff.Lineage
	var firstNext string
	recoveryCheck = func(t *testing.T, stage string, db *pgxpool.Pool, w scheduler.Store, l handoff.Lineage, observed *monitoring.Result) {
		ctx := context.Background()
		if stage == "claimed" {
			if controller.DB == nil {
				controller = recoveryController
				cp = controller.DB.(*pgxpool.Pool)
			}
			if mode == "pause" || mode == "patch" || mode == "owner" || mode == "expiry" {
				recoveryPatch = func(t *testing.T, f memberFixture, m monitor.Public) {
					st := monitor.Store{Pool: db}
					switch mode {
					case "pause":
						a := activationRead(t, db, f.od, m)
						if _, e := st.ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "disabled", ConfigurationVersion: m.Version}, monitor.ActivationETag(a)); e != nil {
							t.Fatal(e)
						}
					case "patch":
						name := "recovery-new-version"
						if _, e := st.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &name}, monitor.ETag(m)); e != nil {
							t.Fatal(e)
						}
					case "expiry":
						var until time.Time
						if e := db.QueryRow(ctx, `SELECT expires_at FROM monitor_schedules WHERE monitor_id=$1`, m.ID).Scan(&until); e != nil {
							t.Fatal(e)
						}
						t.Log("real 60-second authorization expiry", until)
						time.Sleep(time.Until(until) + 20*time.Millisecond)
					case "owner":
						if _, e := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			return
		}
		reason := "worker_lost"
		if mode == "target-ack" || mode == "start-ack" || mode == "input-ack" || mode == "ack" {
			reason = "commit_unknown"
		}
		if mode == "zombie" {
			reason = "lease_expired"
		}
		if mode == "no-permit" || mode == "ambiguous-denial" || mode == "quota-denial" || mode == "recorded-denial" || mode == "stop-only" {
			reason = "egress_unavailable"
		}
		if recoveryCycle == 1 {
			if stage == "before-stop" {
				return
			}
			if stage != "released" {
				t.Fatal("second cycle not physically released", stage)
			}
			if l.RunID == firstLine.RunID || l.NamespaceIncarnation == firstLine.NamespaceIncarnation || l.FencingToken == firstLine.FencingToken || l.RunID != firstNext {
				t.Fatal("recovery replayed old lineage")
			}
			// A second successful GET is a different Run, not compensation for unknown.
			recoveryAssert(t, db, firstLine, "unknown", true)
			recoveryAssert(t, db, l, "started", true)
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, l.MonitorID) != 2 {
				t.Fatal("wrong cycle count")
			}
			t.Log("two real cycles, fresh Run/fence/namespace; old unknown unchanged", firstLine, l)
			return
		}
		if stage == "before-stop" && (mode == "no-permit" || mode == "ambiguous-denial" || mode == "quota-denial" || mode == "recorded-denial" || mode == "stop-only") {
			if mode == "no-permit" && queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4 AND stop_reason IS NULL`, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID) != 1 {
				t.Fatal("unrecorded denial unexpectedly has a stored cause")
			}
			if mode != "no-permit" {
				controlReason := "commit_unknown"
				kind := "permit_denied"
				if mode == "quota-denial" {
					controlReason = "resource_limited"
				}
				if mode == "recorded-denial" || mode == "stop-only" {
					controlReason = "egress_unavailable"
				}
				if mode == "stop-only" {
					kind = "physical_stop_unconfirmed"
				}
				v, err := scheduler.NewControlOutcome(l, kind, controlReason)
				if err != nil {
					t.Fatal(err)
				}
				if err = w.PersistControlResult(ctx, v); err != nil {
					t.Fatal(err)
				}
				var raw []byte
				if err = db.QueryRow(ctx, `SELECT jsonb_build_object('attempt_state',r.attempt_state,'stop_reason',r.stop_reason,'result_commit_id',r.result_commit_id,'target_permit_id',q.target_permit_id,'physical_release_confirmed_at',r.physical_release_confirmed_at) FROM monitor_runs r JOIN scheduler_reservations q USING(workspace_id,project_id,monitor_id,run_id) WHERE r.workspace_id=$1 AND r.project_id=$2 AND r.monitor_id=$3 AND r.run_id=$4`, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var fact struct {
					AttemptState string  `json:"attempt_state"`
					StopReason   string  `json:"stop_reason"`
					ResultID     *string `json:"result_commit_id"`
					PermitID     *string `json:"target_permit_id"`
					ReleasedAt   *string `json:"physical_release_confirmed_at"`
				}
				if json.Unmarshal(raw, &fact) != nil || fact.AttemptState != "started" || fact.StopReason != controlReason || fact.ResultID != nil || fact.PermitID != nil || fact.ReleasedAt != nil {
					t.Fatal("control fact is not a denial attestation", string(raw))
				}
				t.Log("actual control writer kind; identical stored shape does not attest denial", kind, string(raw))
			}
			return
		}
		if stage == "before-stop" && (mode == "race-result-first" || mode == "race-recovery-first") {
			return
		}
		if stage == "before-stop" || stage == "no-input" {
			if mode == "partition" {
				recoveryExchange("partition", nil)
				if _, err := controller.RecoverStarted(ctx, l, reason); err == nil {
					t.Fatal("partition silently succeeded")
				}
				recoveryExchange("partition-witness", nil)
				recoveryAssert(t, db, l, "started", false)
			}
			if mode == "ack" {
				rollback := controller
				rollback.DB = workspaceObservedDB{Pool: cp, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "rollback"} }}
				if _, e := rollback.RecoverStarted(ctx, l, reason); !errors.Is(e, scheduler.ErrCommitUnknown) {
					t.Fatal("rollback must remain unknown to caller", e)
				}
				recoveryAssert(t, db, l, "started", false)
				lost := controller
				lost.DB = workspaceObservedDB{Pool: cp, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
				if _, e := lost.RecoverStarted(ctx, l, reason); !errors.Is(e, scheduler.ErrCommitUnknown) {
					t.Fatal("lost ACK", e)
				}
			}
			before := recoveryBudget(t, db)
			outcome, e := controller.RecoverStarted(ctx, l, reason)
			if e != nil || outcome.AttemptState != "unknown" || outcome.Released || outcome.NextRunID != nil {
				t.Fatal("conservative unresolved recovery", outcome, e)
			}
			recoveryExpected = "unknown"
			recoveryAssert(t, db, l, "unknown", false)
			if recoveryBudget(t, db) != before {
				t.Fatal("recovery returned/refilled physical budget")
			}
			if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND reservation_state='quarantined' AND termination_receipt IS NULL`, l.RunID) != 1 {
				t.Fatal("quarantine lost")
			}
			if _, e := w.ReconcileClaim(ctx, scheduler.ClaimUnknown{Hint: scheduler.Hint{WorkspaceID: l.WorkspaceID, ProjectID: l.ProjectID, MonitorID: l.MonitorID, RunID: l.RunID, ConfigurationVersion: l.ConfigurationVersion, ActivationGeneration: l.ActivationGeneration, SchedulerEpoch: l.SchedulerEpoch}}); e == nil {
				t.Fatal("unknown resurrected lease")
			}
			if stage == "no-input" {
				return
			}
		}
		if stage == "unconfirmed" {
			recoveryAssert(t, db, l, "unknown", false)
			if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
				t.Fatal("unconfirmed returned slot")
			}
			return
		}
		if stage != "released" {
			return
		}
		if mode == "race-result-first" || mode == "race-recovery-first" {
			recoveryRace(t, db, w, controller, l, observed, mode == "race-result-first")
			return
		}
		before := recoveryBudget(t, db)
		var armedID string
		if mode == "ack" {
			ledger := minimalLedger(t, db)
			rollback := controller
			rollback.DB = workspaceObservedDB{Pool: cp, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "rollback"} }}
			if _, err := rollback.RecoverStarted(ctx, l, reason); !errors.Is(err, scheduler.ErrCommitUnknown) {
				t.Fatal("future rollback ACK", err)
			}
			if minimalLedger(t, db) != ledger {
				t.Fatal("rolled-back future identity leaked")
			}
			lost := controller
			lost.DB = workspaceObservedDB{Pool: cp, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
			if _, err := lost.RecoverStarted(ctx, l, reason); !errors.Is(err, scheduler.ErrCommitUnknown) {
				t.Fatal("future committed ACK loss", err)
			}
			if err := db.QueryRow(ctx, `SELECT next_run_id::text FROM monitor_schedules WHERE monitor_id=$1`, l.MonitorID).Scan(&armedID); err != nil || armedID == l.RunID {
				t.Fatal("future did not actually commit", err)
			}
		}
		outcome, e := controller.RecoverStarted(ctx, l, reason)
		want := "unknown"
		if e != nil || outcome.AttemptState != want || !outcome.Released {
			t.Fatal("released recovery", outcome, e)
		}
		if armedID != "" && (outcome.NextRunID == nil || *outcome.NextRunID != armedID) {
			t.Fatal("ACK retry generated a second future identity")
		}
		recoveryAssert(t, db, l, want, true)
		if recoveryBudget(t, db) != before {
			t.Fatal("recovery refunded tokens/slots")
		}
		blocked := mode == "pause" || mode == "patch" || mode == "owner" || mode == "expiry"
		if blocked != (outcome.NextRunID == nil) {
			t.Fatal("current authorization gate", outcome)
		}
		if !blocked && (outcome.NextDueAt == nil || *outcome.NextRunID == l.RunID || time.Until(*outcome.NextDueAt) < 59*time.Second) {
			t.Fatal("not a new future cycle", outcome)
		}
		schedule := minimalLedger(t, db)
		again, e := controller.RecoverStarted(ctx, l, "emergency_stop")
		if e != nil || !sameRecovery(outcome, again) || minimalLedger(t, db) != schedule {
			t.Fatal("recovery replay mutated schedule/terminal fact", again, e)
		}
		if observed != nil {
			commitCall(t, w, commitArgs(w, l, observed), true, "P0001")
		}
		if mode == "validation" {
			// Both parent chains really exist. A valid other tenant must not make this
			// original Run writable through its equally valid but different triple.
			var other monitor.Public
			if !t.Run("other-tenant", func(t *testing.T) { _, other = monitorFixture(t, db) }) {
				return
			}
			a := recoveryArgs(controller.Incarnation, l, reason)
			a[1], a[2], a[3] = other.WorkspaceID, other.ProjectID, other.ID
			var denied []byte
			budgetSQLState(t, cp.QueryRow(ctx, recoverSQL, a...).Scan(&denied), "P0001")
			a = recoveryArgs("77777777-7777-4777-8777-777777777777", l, reason)
			budgetSQLState(t, cp.QueryRow(ctx, recoverSQL, a...).Scan(&denied), "42501")
			for i := 1; i < 11; i++ {
				a := recoveryArgs(controller.Incarnation, l, reason)
				if i >= 5 && i <= 8 {
					a[i] = "999"
				} else {
					a[i] = "77777777-7777-4777-8777-777777777777"
				}
				var raw []byte
				e := cp.QueryRow(ctx, recoverSQL, a...).Scan(&raw)
				budgetSQLState(t, e, "P0001")
			}
			var raw []byte
			budgetSQLState(t, w.DB.QueryRow(ctx, recoverSQL, recoveryArgs(controller.Incarnation, l, reason)...).Scan(&raw), "42501")
			const sig = "scheduler_private.recover_started(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text)"
			for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_controller", "devpilot_scheduler_supervisor", "devpilot_scheduler_api"} {
				var yes bool
				if e := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, sig).Scan(&yes); e != nil || yes != (role == "devpilot_scheduler_controller") {
					t.Fatal("exact ACL", role, yes, e)
				}
			}
			t.Log("exact Controller-only EXECUTE; mixed scope/lineage/fence/incarnations refused")
		}
		firstLine = l
		if outcome.NextRunID != nil {
			firstNext = *outcome.NextRunID
		}
	}
	runRecoveryKernel(t)
}
func recoveryBudget(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var raw []byte
	if e := db.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func sameRecovery(a, b scheduler.Recovery) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func TestRecoveryUnknownPG(t *testing.T)       { runRecovery(t, "unknown") }
func TestRecoveryNoPermitPG(t *testing.T)      { runRecovery(t, "no-permit") }
func TestRecoveryTargetACKPG(t *testing.T)     { runRecovery(t, "target-ack") }
func TestRecoveryStartACKPG(t *testing.T)      { runRecovery(t, "start-ack") }
func TestRecoveryInputACKPG(t *testing.T)      { runRecovery(t, "input-ack") }
func TestRecoveryUnconfirmedPG(t *testing.T)   { runRecovery(t, "unconfirmed") }
func TestRecoveryCommitACKPG(t *testing.T)     { runRecovery(t, "ack") }
func TestRecoveryValidationPG(t *testing.T)    { runRecovery(t, "validation") }
func TestRecoveryPausePG(t *testing.T)         { runRecovery(t, "pause") }
func TestRecoveryPatchPG(t *testing.T)         { runRecovery(t, "patch") }
func TestRecoveryOwnerPG(t *testing.T)         { runRecovery(t, "owner") }
func TestRecoveryZombiePG(t *testing.T)        { runRecovery(t, "zombie") }
func TestRecoveryNextCyclePG(t *testing.T)     { runRecovery(t, "cycle") }
func TestRecoveryResultFirstPG(t *testing.T)   { runRecovery(t, "race-result-first") }
func TestRecoveryRecoveryFirstPG(t *testing.T) { runRecovery(t, "race-recovery-first") }

// Two independent SQL connections queue in both orders behind a real parent
// barrier. Capture pg_locks/blockers while waiting, print after release only.
func recoveryRace(t *testing.T, db *pgxpool.Pool, w, c scheduler.Store, l handoff.Lineage, r *monitoring.Result, resultFirst bool) {
	t.Helper()
	ctx := context.Background()
	barrier, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(ctx)
	if _, e = barrier.Exec(ctx, `SELECT 1 FROM workspaces WHERE id=$1 FOR UPDATE`, l.WorkspaceID); e != nil {
		t.Fatal(e)
	}
	type answer struct {
		result bool
		err    error
		at     time.Time
	}
	done := make(chan answer, 2)
	pids := []int{}
	for i := 0; i < 2; i++ {
		result := (i == 0) == resultFirst
		pool := c.DB.(*pgxpool.Pool)
		if result {
			pool = w.DB.(*pgxpool.Pool)
		}
		conn, e := pool.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		pid := int(conn.Conn().PgConn().PID())
		pids = append(pids, pid)
		go func() {
			defer conn.Release()
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err == nil {
				defer tx.Rollback(ctx)
				var raw []byte
				if result {
					err = tx.QueryRow(ctx, commitSQL, commitArgs(w, l, r)...).Scan(&raw)
				} else {
					err = tx.QueryRow(ctx, recoverSQL, recoveryArgs(c.Incarnation, l, "worker_lost")...).Scan(&raw)
				}
				if err == nil {
					err = tx.Commit(ctx)
				}
			}
			done <- answer{result, err, time.Now()}
		}()
		waitWorkspaceBlock(t, db, pid)
	}
	locks := schedulerLockSnapshot(t, db, "result-versus-recovery-parent-queue", pids...)
	if e = barrier.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log(string(locks))
	for i := 0; i < 2; i++ {
		a := <-done
		t.Log("terminal commit order", a.result, a.at)
		want := "00000"
		if a.result && !resultFirst {
			want = "P0001"
		}
		budgetSQLState(t, a.err, want)
	}
	if resultFirst {
		if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='completed' AND result_commit_id IS NOT NULL AND http_status=200`, l.RunID) != 1 {
			t.Fatal("confirmed result lost")
		}
	} else {
		recoveryAssert(t, db, l, "unknown", true)
	}
}

func TestRecoveryPartitionPG(t *testing.T) { runRecovery(t, "partition") }
func TestRecoveryExpiryPG(t *testing.T)    { runRecovery(t, "expiry") }
func TestRecoveryEmergencyPG(t *testing.T) { runRecovery(t, "emergency") }
func recoveryHint(t *testing.T, db *pgxpool.Pool, f memberFixture, m monitor.Public) schedulerClaimHint {
	if recoveryMode != "expiry" {
		return budgetHint(t, db, f, m)
	}
	a := activationRead(t, db, f.od, m)
	if _, e := (monitor.Store{Pool: db}).ExecutionAuthorize(context.Background(), f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: m.Version, ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a)); e != nil {
		t.Fatal(e)
	}
	var h schedulerClaimHint
	if e := db.QueryRow(context.Background(), `SELECT s.workspace_id::text,s.project_id::text,s.monitor_id::text,b.user_id::text,b.membership_id::text,s.authorized_configuration_version::text,s.authorized_activation_generation::text,s.scheduler_epoch::text,s.next_run_id::text FROM monitor_schedules s JOIN workspace_members b ON b.workspace_id=s.workspace_id AND b.user_id=$2 WHERE s.monitor_id=$1`, m.ID, f.owner.ID).Scan(&h.w, &h.p, &h.m, &h.u, &h.b, &h.v, &h.g, &h.e, &h.r); e != nil {
		t.Fatal(e)
	}
	return h
}

func TestRecoveryAmbiguousDenialPG(t *testing.T) { runRecovery(t, "ambiguous-denial") }

func TestRecoveryQuotaDeniedPG(t *testing.T) { runRecovery(t, "quota-denial") }

// The same persisted reason can come from two different accepted control kinds.
// Real kernel denial/zero TCP and physical stop do not restore that lost origin.
func TestRecoveryRecordedDenialPG(t *testing.T) { runRecovery(t, "recorded-denial") }
func TestRecoveryStopOnlyPG(t *testing.T)       { runRecovery(t, "stop-only") }
