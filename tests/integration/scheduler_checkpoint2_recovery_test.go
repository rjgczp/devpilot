//go:build integration && schedulerfixture && linux

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

func checkpointTwoHint(h schedulerClaimHint) scheduler.Hint {
	return scheduler.Hint{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m, OwnerUserID: h.u, MembershipID: h.b, ConfigurationVersion: h.v, ActivationGeneration: h.g, SchedulerEpoch: h.e, RunID: h.r}
}
func TestCheckpointTwoClaimLostAcknowledgement(t *testing.T) {
	for _, outcome := range []string{"lost", "rollback"} {
		t.Run(outcome, func(t *testing.T) {
			db := workspaceDB(t)
			_, _, h := schedulerClaimFixture(t, db)
			w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
			w2, inc2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
			schedulerDatabaseOnlyGate(t, db)
			// The wrapper REALLY commits or rolls back on an independent LOGIN
			// connection before discarding its acknowledgement. It mocks no SQL.
			uncertainStore := scheduler.Store{DB: workspaceObservedDB{Pool: w, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: outcome} }}, Incarnation: inc}
			lease, err := uncertainStore.Claim(context.Background(), checkpointTwoHint(h))
			var unknown scheduler.ClaimUnknown
			if !errors.As(err, &unknown) || lease.RunID != "" {
				t.Fatal("unknown commit exposed a lease", err, lease)
			}
			store := scheduler.Store{DB: w, Incarnation: inc}
			confirmed, err := store.ReconcileClaim(context.Background(), unknown)
			want := 0
			if outcome == "lost" {
				want = 1
				if err != nil || confirmed.RunID != h.r || confirmed.FencingToken != "1" {
					t.Fatal("confirmed original lease", err, confirmed)
				}
			} else if !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("rolled-back claim inferred permission", err)
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != want {
				t.Fatal("unexpected replacement run")
			}
			other := scheduler.Store{DB: w2, Incarnation: inc2}
			if _, err = other.ReconcileClaim(context.Background(), unknown); !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("another incarnation acquired lease", err)
			}
			wrong := unknown
			wrong.Hint.ProjectID = wrong.Hint.WorkspaceID
			if _, err = store.ReconcileClaim(context.Background(), wrong); !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("bare run identity bypassed scope", err)
			}
			t.Logf("real commit outcome=%s original_run=%s confirmed=%v; no retry issued", outcome, h.r, want == 1)
			checkpointTwoFinal(t, db, h)
		})
	}
}

func TestCheckpointTwoUnstartedRealLeaseExpiry(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	store := scheduler.Store{DB: w, Incarnation: inc}
	type held struct {
		h     schedulerClaimHint
		lease scheduler.Lease
		kind  string
		owner string
	}
	var all []held
	var deadline time.Time
	for _, kind := range []string{"never-prepared", "prepared", "started"} {
		t.Run("claim-"+kind, func(t *testing.T) {
			f, _, h := schedulerClaimFixtureSeconds(t, db, 3600)
			lease, err := store.Claim(ctx, checkpointTwoHint(h))
			if err != nil {
				t.Fatal(err)
			}
			if kind != "never-prepared" {
				schedulerPreparedDatabaseFixture(t, db, h)
			}
			if kind == "started" {
				if _, err = schedulerStart(t, w, inc, h); err != nil {
					t.Fatal(err)
				}
				if _, err = store.ReconcileClaim(ctx, scheduler.ClaimUnknown{Hint: checkpointTwoHint(h)}); !errors.Is(err, scheduler.ErrCandidate) {
					t.Fatal("started run reconciled as leased before expiry", err)
				}
			}
			if _, err = store.ExpireUnstarted(ctx, lease); !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("early or started expiry accepted", err)
			}
			all = append(all, held{h, lease, kind, f.owner.ID})
			if lease.LeaseUntil.After(deadline) {
				deadline = lease.LeaseUntil
			}
		})
	}
	if len(all) != 3 {
		t.Fatal("missing test cases")
	}
	// Actual database lease time, not a shortened timeout or a backdated row.
	t.Logf("waiting original DB45s lease deadline=%s", deadline.Format(time.RFC3339Nano))
	waitCtx, cancel := context.WithTimeout(ctx, 46*time.Second)
	defer cancel()
	for {
		var passed bool
		queryCtx, queryCancel := context.WithTimeout(waitCtx, 2*time.Second)
		err := db.QueryRow(queryCtx, `SELECT clock_timestamp()>$1`, deadline).Scan(&passed)
		queryCancel()
		if err != nil {
			t.Fatal("lease DB time", err)
		}
		if passed {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("original lease deadline not reached")
		case <-time.After(100 * time.Millisecond):
		}
	}
	for _, item := range all {
		t.Run("expire-"+item.kind, func(t *testing.T) {
			// Recovery has no active Owner dependency and must never lock a new User.
			if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, item.owner); err != nil {
				t.Fatal(err)
			}
			wrongFence := item.lease
			wrongFence.FencingToken = "0"
			if _, err := store.ExpireUnstarted(ctx, wrongFence); !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("stale fence expiry accepted", err)
			}
			outcome, err := store.ExpireUnstarted(ctx, item.lease)
			if item.kind == "started" {
				if !errors.Is(err, scheduler.ErrCandidate) {
					t.Fatal("started expiry released physical authority", err)
				}
			} else {
				if err != nil || outcome.Released != (item.kind == "never-prepared") {
					t.Fatal("wrong expiry proof", outcome, err)
				}
				if _, err = store.ExpireUnstarted(ctx, item.lease); !errors.Is(err, scheduler.ErrCandidate) {
					t.Fatal("terminal run expired twice", err)
				}
			}
			if _, err = store.ReconcileClaim(ctx, scheduler.ClaimUnknown{Hint: checkpointTwoHint(item.h)}); !errors.Is(err, scheduler.ErrCandidate) {
				t.Fatal("expired or started lease reconciled as runnable", err)
			}
			if item.kind == "never-prepared" {
				if queryInt(t, db, `SELECT count(*) FROM monitor_schedules s JOIN http_monitors m ON m.workspace_id=s.workspace_id AND m.project_id=s.project_id AND m.id=s.monitor_id WHERE s.monitor_id=$1 AND s.next_run_id<>$2 AND s.next_due_at>=clock_timestamp()+interval '59 seconds' AND s.last_fencing_token=1`, item.h.m, item.h.r) != 1 {
					t.Fatal("backlog not folded into future cycle")
				}
				if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND release_basis='never_prepared' AND termination_receipt IS NULL AND reservation_state='released'`, item.h.r) != 1 {
					t.Fatal("fabricated supervisor receipt")
				}
			} else if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND physical_release_confirmed_at IS NULL`, item.h.r) != 1 {
				t.Fatal("prepared authority returned capacity")
			}
			checkpointTwoFinal(t, db, item.h)
		})
	}
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 2 {
		t.Fatal("unsafe or duplicate physical capacity release")
	}
}

func TestCheckpointTwoUnlockedHintsAreNotAuthority(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, _, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, h.u); err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, h.w); err != nil {
		t.Fatal(err)
	}
	store := scheduler.Store{DB: w, Incarnation: inc}
	hints, err := store.Candidates(ctx, "")
	if err != nil || len(hints) != 1 || hints[0].OwnerUserID != h.u {
		t.Fatal("hint scan waited for locks or lost committed hint", err)
	}
	t.Log("unlocked scan returned old committed Owner hint while independent User UPDATE and W lock remained held")
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(ctx, hints[0]); !errors.Is(err, scheduler.ErrCandidate) {
		t.Fatal("lock-free hint treated as authorization", err)
	}
	checkpointTwoFinal(t, db, h)
}

func TestCheckpointTwoNarrowRecoveryCapabilities(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, _, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	for _, signature := range []string{"scheduler_private.reconcile_claim(uuid,uuid,uuid,uuid,uuid)", "scheduler_private.expire_unstarted(uuid,uuid,uuid,uuid,uuid,bigint)"} {
		for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_api", "devpilot_scheduler_controller", "devpilot_scheduler_supervisor"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "devpilot_scheduler_worker") {
				t.Fatal("unexpected capability", role, signature, allowed)
			}
			t.Logf("EXECUTE role=%s signature=%s allowed=%v", role, signature, allowed)
		}
	}
	for _, table := range []string{"monitor_runs", "monitor_schedules", "scheduler_reservations", "scheduler_budget_state"} {
		column := "workspace_id"
		if table == "scheduler_budget_state" {
			column = "in_use"
		}
		_, err := w.Exec(ctx, `UPDATE `+table+` SET `+column+`=`+column+` WHERE false`)
		var p *pgconn.PgError
		if !errors.As(err, &p) || p.Code != "42501" {
			t.Fatal("expected table privilege refusal", table, err)
		}
	}
	lease, err := (scheduler.Store{DB: w, Incarnation: inc}).Claim(ctx, checkpointTwoHint(h))
	if err != nil {
		t.Fatal(err)
	}
	forged := inc[:len(inc)-1] + "f"
	if forged == inc {
		forged = inc[:len(inc)-1] + "0"
	}
	if _, err = (scheduler.Store{DB: w, Incarnation: forged}).ReconcileClaim(ctx, scheduler.ClaimUnknown{Hint: checkpointTwoHint(h)}); !errors.Is(err, scheduler.ErrUnavailable) {
		t.Fatal("unregistered incarnation read lease", err)
	}
	if _, err = (scheduler.Store{DB: w, Incarnation: forged}).ExpireUnstarted(ctx, lease); !errors.Is(err, scheduler.ErrUnavailable) {
		t.Fatal("unregistered incarnation mutated lease", err)
	}
	checkpointTwoFinal(t, db, h)
}
