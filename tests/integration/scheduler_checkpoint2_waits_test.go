//go:build integration

package integration

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestCheckpointTwoParentStagesAndRollback(t *testing.T) {
	stages := []struct{ name, block, before string }{
		{"User", `SELECT id FROM users WHERE id=$1 FOR UPDATE`, "workspaces"},
		{"Membership", `SELECT user_id FROM workspace_members WHERE workspace_id=$1 FOR UPDATE`, "projects"},
		{"Project", `SELECT id FROM projects WHERE id=$1 FOR UPDATE`, "http_monitors"},
		{"Monitor", `SELECT id FROM http_monitors WHERE id=$1 FOR UPDATE`, "monitor_activation_states"},
		{"Activation", `SELECT monitor_id FROM monitor_activation_states WHERE monitor_id=$1 FOR UPDATE`, "monitor_schedules"},
	}
	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			db := schedulerHistoricalDB(t)
			ctx := context.Background()
			_, _, h := schedulerClaimFixture(t, db)
			w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
			schedulerDatabaseOnlyGate(t, db)
			blocker, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			key := h.m
			switch stage.name {
			case "User":
				key = h.u
			case "Membership":
				key = h.w
			case "Project":
				key = h.p
			}
			if _, err = blocker.Exec(ctx, stage.block, key); err != nil {
				t.Fatal(err)
			}
			pid, ch := startSchedulerClaim(t, w, inc, h)
			waitActivationBlock(t, db, pid)
			if queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND relation=$2::regclass AND mode='RowShareLock'`, pid, stage.before) != 0 {
				t.Fatal("child lock attempted before preceding parent")
			}
			schedulerLockReceipt(t, db, "blocked at "+stage.name+" before "+stage.before, pid)
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			checkpointTwoOutcome(t, <-ch, "00000")
			checkpointTwoFinal(t, db, h)
		})
	}
	for _, mode := range []string{"lock-timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db := schedulerHistoricalDB(t)
			ctx := context.Background()
			_, _, h := schedulerClaimFixture(t, db)
			w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
			schedulerDatabaseOnlyGate(t, db)
			blocker, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			if _, err = blocker.Exec(ctx, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			pid, ch := startSchedulerClaim(t, w, inc, h)
			waitActivationBlock(t, db, pid)
			schedulerLockReceipt(t, db, "fault injection "+mode+" after Run before budget", pid)
			code := "55P03"
			if mode == "cancel" {
				code = "57014"
				var canceled bool
				if err = db.QueryRow(ctx, `SELECT pg_cancel_backend($1)`, pid).Scan(&canceled); err != nil || !canceled {
					t.Fatal("cancel", err)
				}
			}
			checkpointTwoOutcome(t, <-ch, code)
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != 0 || queryInt(t, db, `SELECT last_fencing_token FROM monitor_schedules WHERE monitor_id=$1`, h.m) != 0 {
				t.Fatal("failed claim left partial run/fence")
			}
			checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, h.u)
			if queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND locktype IN ('transactionid','tuple')`, pid) != 0 {
				t.Fatal("failed transaction retained locks")
			}
			checkpointTwoFinal(t, db, h)
		})
	}
}

func TestCheckpointTwoScheduleSkip(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	_, _, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	blocker, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT monitor_id FROM monitor_schedules WHERE monitor_id=$1 FOR UPDATE`, h.m); err != nil {
		t.Fatal(err)
	}
	_, err = schedulerClaim(t, w, inc, h)
	var p *pgconn.PgError
	if !errors.As(err, &p) || p.Code != "P0001" || p.Message != "candidate_busy" {
		t.Fatal("Schedule SKIP", err)
	}
	checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, h.u)
	checkpointTwoProbe(t, db, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, false, h.w)
	checkpointTwoFinal(t, db, h)
}

func TestCheckpointTwoLateLocksAndFreshStop(t *testing.T) {
	for _, stage := range []string{"Run", "Budgets", "Reservation", "Control-stop"} {
		t.Run(stage, func(t *testing.T) {
			db := schedulerHistoricalDB(t)
			ctx := context.Background()
			_, _, h := schedulerClaimFixture(t, db)
			w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
			schedulerDatabaseOnlyGate(t, db)
			if stage == "Run" {
				if _, err := schedulerClaim(t, w, inc, h); err != nil {
					t.Fatal(err)
				}
			}
			blocker, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			sql := `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`
			var args []any
			switch stage {
			case "Run":
				sql = `SELECT run_id FROM monitor_runs WHERE run_id=$1 FOR UPDATE`
				args = []any{h.r}
			case "Reservation":
				sql = `LOCK TABLE scheduler_reservations IN ACCESS EXCLUSIVE MODE`
			case "Control-stop":
				sql = `UPDATE scheduler_control SET emergency_stop=true,gate='closed' WHERE control_id='deployment'`
			}
			if _, err = blocker.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
			pid, ch := startSchedulerClaim(t, w, inc, h)
			waitActivationBlock(t, db, pid)
			schedulerLockReceipt(t, db, "explicit late lock barrier "+stage, pid)
			if stage == "Run" && queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND relation='scheduler_budget_state'::regclass AND mode='RowShareLock'`, pid) != 0 {
				t.Fatal("budgets acquired before Run")
			}
			if stage == "Reservation" {
				checkpointTwoProbe(t, db, `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE NOWAIT`, true)
			}
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			code := "00000"
			if stage == "Run" || stage == "Control-stop" {
				code = "P0001"
			}
			checkpointTwoOutcome(t, <-ch, code)
			want := 1
			if stage == "Control-stop" {
				want = 0
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != want {
				t.Fatal("late wait lost atomicity")
			}
			checkpointTwoFinal(t, db, h)
		})
	}
}

func TestCheckpointTwoInjectedExternalDeadlock(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	_, _, h := schedulerClaimFixture(t, db)
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	external, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback(ctx)
	// This ADMINISTRATIVE fault injector deliberately violates the protocol.
	// No runtime role can take this budget-first table path or set this parameter.
	if _, err = external.Exec(ctx, `SET LOCAL deadlock_timeout='100ms'; SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	var ep int
	if err = external.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&ep); err != nil {
		t.Fatal(err)
	}
	wp, ch := startSchedulerClaim(t, w, inc, h)
	waitActivationBlock(t, db, wp)
	rootDone := make(chan error, 1)
	go func() {
		_, e := external.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, h.w)
		rootDone <- e
	}()
	waitActivationBlock(t, db, ep)
	schedulerLockReceipt(t, db, "injected administrator budget->W inversion; runtime maintains parent order", wp, ep)
	rootErr := <-rootDone
	if err = external.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-ch
	sqlstate := func(e error) string {
		if e == nil {
			return "00000"
		}
		var p *pgconn.PgError
		if errors.As(e, &p) {
			t.Logf("SQLSTATE=%s message=%s detail=%s where=%s", p.Code, p.Message, p.Detail, p.Where)
			return p.Code
		}
		t.Fatalf("non-PG failure %v", e)
		return ""
	}
	a, b := sqlstate(rootErr), sqlstate(result.err)
	if !((a == "40P01" && b == "00000") || (a == "00000" && b == "40P01")) {
		t.Fatalf("unexpected deadlock outcomes %s/%s", a, b)
	}
	expected := 0
	if b == "00000" {
		expected = 1
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE monitor_id=$1`, h.m) != expected {
		t.Fatal("deadlock victim left partial claim")
	}
	if b == "40P01" {
		checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, false, h.u)
	}
	checkpointTwoFinal(t, db, h)
}
