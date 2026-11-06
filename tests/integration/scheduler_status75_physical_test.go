//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture && recoveryfixture

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

type status75Observer struct {
	m             monitor.Public
	server        string
	client        *http.Client
	owner, member string
}

func status75PhysicalObserver(t *testing.T, db *pgxpool.Pool, l handoff.Lineage) status75Observer {
	t.Helper()
	ctx := context.Background()
	owner, ot, _ := workspaceIdentity(t, db, "status75-owner@example.com")
	member, mt, _ := workspaceIdentity(t, db, "status75-member@example.com")
	if _, e := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, l.WorkspaceID, owner.ID, member.ID); e != nil {
		t.Fatal(e)
	}
	s, c, _ := checkpointHTTPServer(t, db)
	return status75Observer{m: monitor.Public{Summary: monitor.Summary{WorkspaceID: l.WorkspaceID, ProjectID: l.ProjectID, ID: l.MonitorID, Version: l.ConfigurationVersion}}, server: s.URL, client: c, owner: ot, member: mt}
}
func (o status75Observer) read(t *testing.T, db *pgxpool.Pool) status75Pair {
	t.Helper()
	retentionLedger(t, db) // Schema9: physical slots, pending records and retained audits agree atomically.
	before := minimalLedger(t, db)
	owner := status75ReadPair(t, o.client, o.server, o.m, o.owner)
	member := status75ReadPair(t, o.client, o.server, o.m, o.member)
	if !bytes.Equal(owner.status, member.status) || owner.tag != member.tag || !bytes.Equal(owner.activation, member.activation) {
		t.Fatal("Owner and Member disagree without concurrent writes")
	}
	if minimalLedger(t, db) != before {
		t.Fatal("paired GETs mutated intent, results or physical resources")
	}
	return member
}
func status75Physical(t *testing.T, mode string) {
	defer func() {
		recoveryBeforeSettlement = nil
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
	if mode == "unknown-after-completed" {
		recoveryCycles = 2
	}
	recoveryMode = "status75"
	if mode == "expiry" {
		recoveryMode = "expiry"
	}
	var observer status75Observer
	var initial status75Pair
	var previous string
	if mode == "expiry" {
		recoveryBeforeSettlement = func(t *testing.T, db *pgxpool.Pool, w scheduler.Store, l handoff.Lineage) {
			before := observer.read(t, db)
			if before.s.ExpiresAt == nil || !before.s.InFlight {
				t.Fatal("pending physical confirmation absent")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
			defer cancel()
			for {
				var remaining float64
				if e := db.QueryRow(ctx, `SELECT greatest(0,extract(epoch FROM $1::timestamptz-clock_timestamp()))::float8`, *before.s.ExpiresAt).Scan(&remaining); e != nil {
					t.Fatal(e)
				}
				if remaining == 0 {
					break
				}
				timer := time.NewTimer(time.Duration(remaining * float64(time.Second)))
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					t.Fatal(ctx.Err())
				}
			}
			after := observer.read(t, db)
			if !after.s.InFlight || after.s.AuthorizationState != "expired" || after.s.EffectiveAuthorization || after.s.BlockedReason == nil || *after.s.BlockedReason != "authorization_expired" || after.tag != before.tag || !bytes.Equal(before.activation, after.activation) || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
				t.Fatal("expiry hid unconfirmed execution or returned capacity", after.s)
			}
			got, e := recoveryController.RecoverStarted(ctx, l, "worker_lost")
			if e != nil || got.AttemptState != "unknown" || got.Released {
				t.Fatal("expired started conservative classification", e, got)
			}
			recoveryExpected = "unknown"
		}
	}
	recoveryCheck = func(t *testing.T, stage string, db *pgxpool.Pool, w scheduler.Store, l handoff.Lineage, result *monitoring.Result) {
		ctx := context.Background()
		if stage == "claimed" {
			if recoveryCycle == 0 {
				observer = status75PhysicalObserver(t, db, l)
				initial = observer.read(t, db)
			}
			got := observer.read(t, db)
			if got.s.LatestAttempt == nil || got.s.LatestAttempt.RunID != l.RunID || got.s.LatestAttempt.AttemptState != "leased" || got.s.InFlight {
				t.Fatal("Claim represented as running", got.s)
			}
			if recoveryCycle == 1 && (got.s.LatestCompletedObservation == nil || got.s.LatestCompletedObservation.RunID != previous || got.s.CurrentObservation == nil || got.s.CurrentObservation.RunID != previous) {
				t.Fatal("prior real success absent before unknown", got.s)
			}
			return
		}
		if stage == "before-stop" {
			got := observer.read(t, db)
			if got.s.LatestAttempt.AttemptState != "started" || !got.s.InFlight {
				t.Fatal("started hidden", got.s)
			}
			if mode == "pause" || mode == "patch" {
				method, path, body, tag := "PUT", status75Base(observer.m)+"/activation", fmt.Sprintf(`{"desired_state":"disabled","configuration_version":%q}`, got.a.ConfigurationVersion), got.tag
				if mode == "patch" {
					method, path, body = "PATCH", status75Base(observer.m), `{"name":"new configuration while old Run in flight"}`
					code, h, raw := checkpointHTTP(t, observer.client, observer.server, "GET", path, "", observer.owner, "", nil)
					if code != 200 {
						t.Fatal(code, string(raw))
					}
					tag = h.Get("ETag")
				}
				code, _, raw := checkpointHTTP(t, observer.client, observer.server, method, path, body, observer.owner, tag, nil)
				if code != 200 {
					t.Fatal("in-flight Owner mutation", code, string(raw))
				}
				after := observer.read(t, db)
				if after.a.ActivationGeneration == got.a.ActivationGeneration || after.s.DesiredState != "disabled" || after.s.SchedulingState != "disabled" || !after.s.InFlight || after.s.LatestAttempt.RunID != l.RunID || after.s.LatestAttempt.ConfigurationVersion != l.ConfigurationVersion || after.s.LatestAttempt.ActivationGeneration != l.ActivationGeneration || after.s.CurrentObservation != nil {
					t.Fatal("old in-flight facts relabeled", after.s)
				}
				if mode == "patch" && after.s.ConfigurationVersion == l.ConfigurationVersion {
					t.Fatal("PATCH version unchanged")
				}
				if checkpointClientObservation("intent-v2", got.a, 200, after.status) != "refresh_status" {
					t.Fatal("old/new intent spliced")
				}
			}
			if mode == "unconfirmed" || (mode == "unknown-after-completed" && recoveryCycle == 1) {
				recovered, e := recoveryController.RecoverStarted(ctx, l, "worker_lost")
				if e != nil || recovered.AttemptState != "unknown" || recovered.Released {
					t.Fatal("conservative unknown", e, recovered)
				}
				recoveryExpected = "unknown"
				got = observer.read(t, db)
				if got.s.LatestAttempt.AttemptState != "unknown" || !got.s.InFlight || got.s.CurrentObservation != nil || got.s.LatestAttempt.HTTPStatus != nil || got.s.LatestAttempt.Success != nil {
					t.Fatal("unknown replaced by success", got.s)
				}
				if previous != "" && (got.s.LatestCompletedObservation == nil || got.s.LatestCompletedObservation.RunID != previous) {
					t.Fatal("prior real audit lost")
				}
			}
			if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
				t.Fatal("GET or pause prematurely released physical slot")
			}
			return
		}
		if stage == "unconfirmed" {
			got := observer.read(t, db)
			if !got.s.InFlight || got.s.LatestAttempt.AttemptState != "unknown" || got.s.CurrentObservation != nil || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND reservation_state='quarantined' AND termination_receipt IS NULL AND released_at IS NULL`, l.RunID) != 1 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
				t.Fatal("unconfirmed stop returned capacity or hid unknown", got.s)
			}
			return
		}
		if stage != "released" {
			t.Fatal("unexpected lifecycle stage", stage)
		}
		got := observer.read(t, db)
		if got.s.InFlight || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 0 {
			t.Fatal("trusted release did not update physical fact", got.s)
		}
		if recoveryExpected == "unknown" {
			if got.s.LatestAttempt.AttemptState != "unknown" || got.s.CurrentObservation != nil || got.s.LatestAttempt.Success != nil || got.s.LatestAttempt.HTTPStatus != nil || (previous != "" && (got.s.LatestCompletedObservation == nil || got.s.LatestCompletedObservation.RunID != previous)) {
				t.Fatal("release invented HTTP result or masked unknown", got.s)
			}
		} else {
			if got.s.LatestAttempt.AttemptState != "started" {
				t.Fatal("release invented completion")
			}
			v, e := scheduler.NetworkResult(l, result)
			if e != nil {
				t.Fatal(e)
			}
			commit, e := scheduler.NewResultCommit(v)
			if e != nil {
				t.Fatal(e)
			}
			if e = w.CommitResult(ctx, commit); e != nil {
				t.Fatal(e)
			}
			got = observer.read(t, db)
			if got.s.LatestAttempt.AttemptState != "completed" || got.s.LatestCompletedObservation == nil || got.s.LatestCompletedObservation.RunID != l.RunID {
				t.Fatal("completed audit absent", got.s)
			}
			if mode == "pause" || mode == "patch" {
				if got.s.CurrentObservation != nil || got.s.DesiredState != "disabled" {
					t.Fatal("old result polluted current intent", got.s)
				}
			} else if got.s.CurrentObservation == nil || got.s.CurrentObservation.RunID != l.RunID || got.s.CurrentObservation.Success == nil || !*got.s.CurrentObservation.Success {
				t.Fatal("matching current result absent", got.s)
			}
			if mode == "unknown-after-completed" && recoveryCycle == 0 {
				previous = l.RunID
				// Test-only next input to the existing driver, not a scheduler feature:
				// fresh Run ID, full interval AFTER the confirmed release, no catch-up.
				// This creates no Run, lease, permit, physical receipt or HTTP result.
				if _, e = db.Exec(ctx, `UPDATE monitor_schedules SET next_run_id=uuidv7(),next_due_at=greatest(next_start_not_before,clock_timestamp()+interval '60 seconds') WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, l.WorkspaceID, l.ProjectID, l.MonitorID); e != nil {
					t.Fatal(e)
				}
			}
		}
		if mode != "pause" && mode != "patch" && (!bytes.Equal(initial.activation, got.activation) || initial.tag != got.tag) {
			t.Fatal("runtime changed Activation strong representation")
		}
		t.Log("D11.3 actual HTTP pair / original HTTPS kernel / signed physical release / independent Result fact", mode, recoveryCycle)
	}
	runRecoveryKernel(t)
}
func TestExecutionStatusConsistencyCompletedPG(t *testing.T) { status75Physical(t, "completed") }
func TestExecutionStatusConsistencyPausePG(t *testing.T)     { status75Physical(t, "pause") }
func TestExecutionStatusConsistencyPatchPG(t *testing.T)     { status75Physical(t, "patch") }
func TestExecutionStatusConsistencyUnknownPG(t *testing.T) {
	status75Physical(t, "unknown-after-completed")
}
func TestExecutionStatusConsistencyUnconfirmedPG(t *testing.T) { status75Physical(t, "unconfirmed") }

func TestExecutionStatusConsistencyExpiryPG(t *testing.T) { status75Physical(t, "expiry") }
