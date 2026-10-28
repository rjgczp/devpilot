//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture && recoveryfixture

package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

// These tests use the accepted kernel, Supervisor and signed settlement chain.
// No administrative Run UPDATE or forged physical release is used here.
func status74Observer(t *testing.T, db *pgxpool.Pool, l handoff.Lineage) func() monitor.ExecutionStatus {
	t.Helper()
	ctx := context.Background()
	api := status74API(t, db)
	u, token, _ := workspaceIdentity(t, db, "status74-reader@example.com")
	if _, e := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, l.WorkspaceID, u.ID); e != nil {
		t.Fatal(e)
	}
	m := monitor.Public{Summary: monitor.Summary{WorkspaceID: l.WorkspaceID, ProjectID: l.ProjectID, ID: l.MonitorID}}
	return func() monitor.ExecutionStatus { return status74Read(t, api, m, token) }
}
func status74RealResult(t *testing.T, patch bool) {
	defer func() { jointResultCheck = nil; jointResultPatch = nil; jointResultSetup = nil }()
	var read func() monitor.ExecutionStatus
	jointResultCheck = func(t *testing.T, stage string, db *pgxpool.Pool, w scheduler.Store, l handoff.Lineage, r *monitoring.Result) {
		switch stage {
		case "claimed":
			read = status74Observer(t, db, l)
			s := read()
			if s.LatestAttempt == nil || s.LatestAttempt.AttemptState != "leased" || s.InFlight {
				t.Fatal("Claim falsely running", s)
			}
			if patch {
				jointResultPatch = func(t *testing.T, f memberFixture, m monitor.Public) {
					name := "status74-new-version"
					if _, e := (monitor.Store{Pool: db}).Update(context.Background(), f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &name}, monitor.ETag(m)); e != nil {
						t.Fatal(e)
					}
				}
			}
		case "before-stop":
			s := read()
			if s.LatestAttempt.AttemptState != "started" || !s.InFlight || s.CurrentObservation != nil {
				t.Fatal("started physical permission hidden", s)
			}
		case "released":
			s := read()
			if s.InFlight || s.LatestAttempt.AttemptState != "started" || s.CurrentObservation != nil {
				t.Fatal("physical release invented HTTP result", s)
			}
			v, e := scheduler.NetworkResult(l, r)
			if e != nil {
				t.Fatal(e)
			}
			c, e := scheduler.NewResultCommit(v)
			if e != nil {
				t.Fatal(e)
			}
			if e = w.CommitResult(context.Background(), c); e != nil {
				t.Fatal(e)
			}
			before := minimalLedger(t, db)
			s = read()
			if minimalLedger(t, db) != before {
				t.Fatal("status mutated resource/intent ledger")
			}
			if s.LatestAttempt.AttemptState != "completed" || s.LatestCompletedObservation == nil || s.LatestCompletedObservation.RunID != l.RunID || s.InFlight {
				t.Fatal("completed audit missing", s)
			}
			if patch {
				if s.CurrentObservation != nil || s.ConfigurationVersion == l.ConfigurationVersion || s.DesiredState != "disabled" {
					t.Fatal("old Run polluted new current observation", s)
				}
			} else {
				if s.CurrentObservation == nil || s.CurrentObservation.RunID != l.RunID || s.CurrentObservation.Success == nil || !*s.CurrentObservation.Success {
					t.Fatal("confirmed current observation missing", s)
				}
			}
			t.Log("status74 true Header-only HTTPS/physical release/Result Commit/HTTP projection", stage, patch)
		default:
			t.Fatal("unexpected physical stage", stage)
		}
	}
	TestBudgetJointKernelPG(t)
}
func TestExecutionStatusRealCompletedPG(t *testing.T) { status74RealResult(t, false) }
func TestExecutionStatusRealOldResultPG(t *testing.T) { status74RealResult(t, true) }
func TestExecutionStatusRealUnknownPG(t *testing.T) {
	defer func() {
		recoveryCheck = nil
		recoveryPatch = nil
		recoverySetup = nil
		recoveryController = scheduler.Store{}
		recoveryExpected = "started"
		recoveryCycle = 0
		recoveryCycles = 1
		recoveryExchange = nil
		recoveryMode = ""
	}()
	recoveryCycles = 1
	recoveryExpected = "started"
	recoveryMode = "status74"
	var read func() monitor.ExecutionStatus
	recoveryCheck = func(t *testing.T, stage string, db *pgxpool.Pool, w scheduler.Store, l handoff.Lineage, r *monitoring.Result) {
		switch stage {
		case "claimed":
			read = status74Observer(t, db, l)
		case "before-stop":
			got, e := recoveryController.RecoverStarted(context.Background(), l, "worker_lost")
			if e != nil || got.AttemptState != "unknown" || got.Released {
				t.Fatal("real recovery classification", e, got)
			}
			recoveryExpected = "unknown"
			s := read()
			if s.LatestAttempt.AttemptState != "unknown" || !s.InFlight || s.CurrentObservation != nil || s.LatestAttempt.Success != nil || s.LatestAttempt.HTTPStatus != nil {
				t.Fatal("unknown before physical release", s)
			}
		case "released":
			before := minimalLedger(t, db)
			s := read()
			if minimalLedger(t, db) != before {
				t.Fatal("read released capacity")
			}
			if s.LatestAttempt.AttemptState != "unknown" || s.InFlight || s.CurrentObservation != nil || s.LatestAttempt.Success != nil || s.LatestAttempt.HTTPStatus != nil {
				t.Fatal("physical release relabeled unknown", s)
			}
			t.Log("status74 unknown true->false in_flight backed by independent signed physical release")
		default:
			t.Fatal("unexpected unknown stage", stage)
		}
	}
	runRecoveryKernel(t)
}
