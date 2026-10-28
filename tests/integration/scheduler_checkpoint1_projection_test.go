//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
)

// Executable consumer contract, not a frontend or a deployed-client claim.
// A missing/failed authority and different snapshots never imply idle/healthy.
func checkpointClientObservation(header string, activation monitor.Activation, code int, raw []byte) string {
	if header != "intent-v2" || code != 200 {
		return "运行状态不可用"
	}
	var response struct{ Data monitor.ExecutionStatus }
	if json.Unmarshal(raw, &response) != nil {
		return "运行状态不可用"
	}
	s := response.Data
	if s.MonitorID == "" || s.PauseSemantics != "start_commit" {
		return "运行状态不可用"
	}
	if s.WorkspaceID != activation.WorkspaceID || s.ProjectID != activation.ProjectID || s.MonitorID != activation.MonitorID || s.ConfigurationVersion != activation.ConfigurationVersion || s.ActivationGeneration != activation.ActivationGeneration {
		return "refresh_status"
	}
	if s.LatestAttempt != nil {
		return s.LatestAttempt.AttemptState
	}
	return s.SchedulingState
}

func checkpointProjectionGet(t *testing.T, c *http.Client, server, path, token string) (monitor.Activation, http.Header, []byte) {
	t.Helper()
	code, h, raw := checkpointHTTP(t, c, server, "GET", path+"/activation", "", token, "", nil)
	var a struct{ Data monitor.Activation }
	if code != 200 || json.Unmarshal(raw, &a) != nil {
		t.Fatal("activation projection", code)
	}
	if a.Data.ExecutionState != "not_reported" || a.Data.BlockedReason != "execution_status_required" || h.Get("DevPilot-Activation-Projection") != "intent-v2" {
		t.Fatal("migration hints changed")
	}
	var shape struct{ Data map[string]any }
	json.Unmarshal(raw, &shape)
	if len(shape.Data) != 14 || shape.Data["next_due_at"] != nil {
		t.Fatal("Activation shape changed")
	}
	return a.Data, h, raw
}

func TestCheckpointOneHTTPIntentStatusContract(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	server, client, _ := checkpointHTTPServer(t, db)
	for _, state := range []string{"disabled", "blocked", "authorized_waiting", "due", "leased", "started", "unknown", "completed", "configuration_changed", "expired"} {
		t.Run(state, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			s := monitor.Store{Pool: db}
			path := "/api/v1/workspaces/" + m.WorkspaceID + "/projects/" + m.ProjectID + "/monitors/" + m.ID
			if state != "disabled" {
				a := activationRead(t, db, f.od, m)
				if _, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a)); err != nil {
					t.Fatal(err)
				}
			}
			if state != "disabled" && state != "blocked" {
				if _, err := db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment';`); err != nil {
					t.Fatal(err)
				}
			}
			if state == "authorized_waiting" {
				if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET next_due_at=clock_timestamp()+interval '1 minute' WHERE monitor_id=$1`, m.ID); err != nil {
					t.Fatal(err)
				}
			}
			if state == "due" {
				if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET next_due_at=clock_timestamp() WHERE monitor_id=$1`, m.ID); err != nil {
					t.Fatal(err)
				}
			}
			if state == "expired" {
				if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET issued_at=clock_timestamp()-interval '61 seconds',expires_at=clock_timestamp()-interval '1 second',scheduler_epoch=scheduler_epoch+1 WHERE monitor_id=$1`, m.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, headers, body := checkpointProjectionGet(t, client, server.URL, path, f.memberToken)
			// Administrative row fixtures validate HTTP projection only. No result
			// commit, physical release or actual network execution is claimed here.
			if state == "leased" || state == "started" || state == "unknown" || state == "completed" || state == "configuration_changed" {
				var run string
				if err := db.QueryRow(ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp() n) INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state) SELECT s.workspace_id,s.project_id,s.monitor_id,uuidv7(),s.authorized_configuration_version,s.authorized_activation_generation,s.scheduler_epoch,1,n,n,session_user,uuidv7(),n+interval '45 seconds','leased' FROM monitor_schedules s CROSS JOIN t WHERE s.monitor_id=$1 RETURNING run_id::text`, m.ID).Scan(&run); err != nil {
					t.Fatal(err)
				}
				if state != "leased" {
					if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='started',started_at=clock_timestamp() WHERE run_id=$1`, run); err != nil {
						t.Fatal(err)
					}
				}
				if state == "unknown" {
					if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='unknown',finished_at=clock_timestamp(),stop_reason='worker_lost' WHERE run_id=$1`, run); err != nil {
						t.Fatal(err)
					}
				}
				if state == "completed" {
					if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='completed',finished_at=clock_timestamp(),result_commit_id=uuidv7(),duration_ms=1,http_status=200,success=true,physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, run); err != nil {
						t.Fatal(err)
					}
					// Administrative HTTP projection fixture only: model the pointer
					// a real accepted Result Commit sets. This is not a stop proof.
					if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET latest_result_run_id=$1 WHERE workspace_id=$2 AND project_id=$3 AND monitor_id=$4`, run, m.WorkspaceID, m.ProjectID, m.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if state == "configuration_changed" {
				if _, err := s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("new configuration"), monitor.ETag(m)); err != nil {
					t.Fatal(err)
				}
			}
			current, h, after := checkpointProjectionGet(t, client, server.URL, path, f.memberToken)
			if state != "configuration_changed" && (!bytes.Equal(body, after) || headers.Get("ETag") != h.Get("ETag")) {
				t.Fatal("operational change mutated intent validator")
			}
			code, _, raw := checkpointHTTP(t, client, server.URL, "GET", path+"/execution-status", "", f.memberToken, "", nil)
			var observed struct{ Data monitor.ExecutionStatus }
			if code != 200 || json.Unmarshal(raw, &observed) != nil {
				t.Fatal("authoritative HTTP status", code)
			}
			status := observed.Data
			want := state
			switch state {
			case "expired":
				want = "blocked"
				if status.AuthorizationState != "expired" || status.EffectiveAuthorization {
					t.Fatal("expired authorization")
				}
			case "configuration_changed":
				want = "started"
				if status.CurrentObservation != nil || status.SchedulingState != "disabled" || !status.InFlight {
					t.Fatal("PATCH hid old in-flight lineage")
				}
			case "unknown":
				if !status.InFlight || status.CurrentObservation != nil || status.LatestAttempt.Success != nil || status.LatestAttempt.HTTPStatus != nil {
					t.Fatal("unknown falsified HTTP result")
				}
			case "completed":
				if status.CurrentObservation == nil || status.InFlight {
					t.Fatal("matching completed observation missing")
				}
			}
			if got := checkpointClientObservation(h.Get("DevPilot-Activation-Projection"), current, code, raw); got != want {
				t.Fatalf("client observed %q want %q", got, want)
			}
			for _, badCode := range []int{0, 404, 503} {
				if checkpointClientObservation("intent-v2", current, badCode, raw) != "运行状态不可用" {
					t.Fatal("client inferred state without authority")
				}
			}
			if checkpointClientObservation("", current, 200, raw) != "运行状态不可用" || checkpointClientObservation("intent-v2", current, 200, []byte(`{}`)) != "运行状态不可用" {
				t.Fatal("unsupported/invalid authority inferred execution")
			}
			if state == "configuration_changed" && checkpointClientObservation("intent-v2", before, 200, raw) != "refresh_status" {
				t.Fatal("client merged snapshots across PATCH")
			}
			for _, token := range []string{"", f.otherToken} {
				code, _, _ := checkpointHTTP(t, client, server.URL, "GET", path+"/execution-status", "", token, "", nil)
				want := 401
				if token != "" {
					want = 404
				}
				if code != want {
					t.Fatal("status real scope/identity", code)
				}
			}
			code, _, _ = checkpointHTTP(t, client, server.URL, "GET", path+"/execution-status", "", f.ownerToken, "", nil)
			if code != 200 {
				t.Fatal("Owner status read")
			}
			// Tear down only fabricated database rows: no physical Probe was created.
			if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state=CASE WHEN started_at IS NULL THEN 'canceled' ELSE CASE WHEN attempt_state='started' THEN 'unknown' ELSE attempt_state END END,finished_at=coalesce(finished_at,clock_timestamp()),physical_release_confirmed_at=clock_timestamp() WHERE monitor_id=$1 AND physical_release_confirmed_at IS NULL`, m.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type checkpointLegacyResponse struct{ http.ResponseWriter }

type checkpointLegacyRepository struct{ monitor.Store }

func (s checkpointLegacyRepository) ActivationDetail(ctx context.Context, d session.Digest, w, p, m string) (monitor.Activation, error) {
	return (monitor.SchemaSevenFixtureStore(s.Store)).ActivationDetail(ctx, d, w, p, m)
}
func (s checkpointLegacyRepository) ActivationUpdate(ctx context.Context, d session.Digest, w, p, m string, input monitor.ActivationInput, tag string) (monitor.Activation, error) {
	return (monitor.SchemaSevenFixtureStore(s.Store)).ActivationUpdate(ctx, d, w, p, m, input, tag)
}

func (w checkpointLegacyResponse) WriteHeader(code int) {
	w.Header().Del("DevPilot-Activation-Projection")
	w.ResponseWriter.WriteHeader(code)
}

// Historical Activation handler differs only by the added projection header;
// historical Store SQL is independently frozen and hash-checked by the suite.
func checkpointHistoricalHandler(db *pgxpool.Pool) http.Handler {
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := monitor.New(checkpointLegacyRepository{monitor.Store{Pool: db}}, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/execution-status") || strings.HasSuffix(r.URL.Path, "/execution-authorization") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(checkpointLegacyResponse{w}, r)
	})
}

func TestCheckpointOneHTTPCutoverAllTagsAndRollback(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "up-to 7", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	old, oldClient := checkpointHTTPHandlerServer(t, checkpointHistoricalHandler(db))
	type item struct {
		f         memberFixture
		m         monitor.Public
		tag, path string
	}
	var items []item
	f, disabled := monitorFixture(t, db)
	for _, enabled := range []bool{false, true} {
		m := disabled
		if enabled {
			var err error
			m, err = (monitor.Store{Pool: db}).Create(ctx, f.od, disabled.WorkspaceID, disabled.ProjectID, monitorInput("historical enabled"))
			if err != nil {
				t.Fatal(err)
			}
		}
		a, err := (monitor.SchemaSevenFixtureStore{Pool: db}).ActivationDetail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if enabled {
			a, err = activationPut(monitor.SchemaSevenFixtureStore{Pool: db}, f.od, m, a, "enabled")
			if err != nil {
				t.Fatal(err)
			}
		}
		path := "/api/v1/workspaces/" + m.WorkspaceID + "/projects/" + m.ProjectID + "/monitors/" + m.ID
		code, h, raw := checkpointHTTP(t, oldClient, old.URL, "GET", path+"/activation", "", f.ownerToken, "", nil)
		var legacy struct{ Data monitor.Activation }
		if code != 200 || json.Unmarshal(raw, &legacy) != nil || legacy.Data.ExecutionState != "blocked" || legacy.Data.BlockedReason != "scheduler_unavailable" || h.Get("DevPilot-Activation-Projection") != "" {
			t.Fatal("historical HTTP fixture changed")
		}
		items = append(items, item{f, m, h.Get("ETag"), path})
	}
	old.Close() // Simulated maintenance-window HTTP drain before the real migration.
	goose(t, dir, "up-to 8", true)
	server, client, _ := checkpointHTTPServer(t, db)
	for i, it := range items {
		a, h, _ := checkpointProjectionGet(t, client, server.URL, it.path, it.f.ownerToken)
		if h.Get("ETag") == it.tag {
			t.Fatal("old tag survived representation change")
		}
		code, _, _ := checkpointHTTP(t, client, server.URL, "PUT", it.path+"/activation", `{"desired_state":"disabled","configuration_version":"1"}`, it.f.ownerToken, it.tag, nil)
		if code != 412 {
			t.Fatal("old activation HTTP tag accepted", code)
		}
		code, _, _ = checkpointHTTP(t, client, server.URL, "PUT", it.path+"/execution-authorization", `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":60}`, it.f.ownerToken, it.tag, nil)
		if code != 412 {
			t.Fatal("old authorization HTTP tag accepted", code)
		}
		items[i].tag = monitor.ActivationETag(a)
		// Disposable Down closes the ledger only when no physical rights exist.
	}
	server.Close()
	goose(t, dir, "down", true)
	old, oldClient = checkpointHTTPHandlerServer(t, checkpointHistoricalHandler(db))
	for _, it := range items {
		code, h, raw := checkpointHTTP(t, oldClient, old.URL, "GET", it.path+"/activation", "", it.f.ownerToken, "", nil)
		var legacy struct{ Data monitor.Activation }
		if code != 200 || json.Unmarshal(raw, &legacy) != nil || legacy.Data.DesiredState != "disabled" || legacy.Data.ExecutionState != "blocked" || h.Get("ETag") == it.tag {
			t.Fatal("rollback revived old intent/tag")
		}
		code, _, raw = checkpointHTTP(t, oldClient, old.URL, "GET", it.path+"/execution-status", "", it.f.ownerToken, "", nil)
		if code != 404 || checkpointClientObservation("", legacy.Data, code, raw) != "运行状态不可用" {
			t.Fatal("rollback client invented execution")
		}
	}
	old.Close()
	goose(t, dir, "up-to 8", true)
}

func TestCheckpointOneProjectionRetirementKeepsSchemaEight(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m, h := schedulerClaimFixture(t, db)
	script, err := os.ReadFile(filepath.Join(root(), "tests/scheduler/retire-projection-fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	retire := func(want bool) {
		t.Helper()
		conn, err := db.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		_, err = conn.Exec(ctx, string(script))
		if (err == nil) != want {
			t.Fatalf("retirement expected-success=%v: %v", want, err)
		}
		if err != nil {
			var p *pgconn.PgError
			if !errors.As(err, &p) || p.Message != "projection_retirement_requires_drain_revoke_stop" {
				t.Fatal("unexpected retirement error", err)
			}
			if _, err = conn.Exec(ctx, "ROLLBACK"); err != nil {
				t.Fatal(err)
			}
		}
	}
	a := activationRead(t, db, f.od, m)
	retire(false) // An outstanding authorization blocks cutover.
	if monitor.ActivationETag(a) != monitor.ActivationETag(activationRead(t, db, f.od, m)) {
		t.Fatal("failed retirement changed tag")
	}
	// Pause closes the logical grant first; there has never been a real Probe.
	if _, err = activationPut(monitor.Store{Pool: db}, f.od, m, a, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO scheduler_control(control_id,kind,login_name,incarnation,routable) VALUES('retirement-http','http',session_user,uuidv7(),true)`); err != nil {
		t.Fatal(err)
	}
	retire(false) // Routable HTTP inventory must be drained.
	if _, err = db.Exec(ctx, `UPDATE scheduler_control SET routable=false WHERE control_id='retirement-http'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp() n) INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,started_at,finished_at,lease_owner,worker_incarnation,lease_until,attempt_state,stop_reason) SELECT $1,$2,$3,$4,$5,$6,$7,1,n,n,n,n,session_user,uuidv7(),n+interval '45 seconds','unknown','worker_lost' FROM t`, h.w, h.p, h.m, h.r, h.v, h.g, h.e); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1 AND physical_release_confirmed_at IS NULL`, h.r); err != nil {
			t.Error(err)
		}
	})
	retire(false) // Lease/unknown is not independent physical-stop proof.
	// Administrator-only disposal of the fabricated row, NOT a real kill receipt.
	if _, err = db.Exec(ctx, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, h.r); err != nil {
		t.Fatal(err)
	}
	before := activationRead(t, db, f.od, m)
	retire(true)
	after := activationRead(t, db, f.od, m)
	if monitor.ActivationETag(before) == monitor.ActivationETag(after) || !before.NextEnableAllowedAt.Equal(*after.NextEnableAllowedAt) || after.DesiredState != "disabled" {
		t.Fatal("retained-schema cutover reused tag or cooldown")
	}
	if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 8 {
		t.Fatal("operational protocol used Goose Down")
	}
	path := "/api/v1/workspaces/" + h.w + "/projects/" + h.p + "/monitors/" + h.m
	old, client := checkpointHTTPHandlerServer(t, checkpointHistoricalHandler(db))
	code, headers, raw := checkpointHTTP(t, client, old.URL, "GET", path+"/activation", "", f.ownerToken, "", nil)
	var historical struct{ Data monitor.Activation }
	if code != 200 || json.Unmarshal(raw, &historical) != nil || historical.Data.ExecutionState != "blocked" || headers.Get("ETag") == monitor.ActivationETag(before) {
		t.Fatal("old projection reused new tag")
	}
	old.Close()
	retire(true) // Switching back to intent-v2 is another representation change.
	newer, c, _ := checkpointHTTPServer(t, db)
	latest, _, _ := checkpointProjectionGet(t, c, newer.URL, path, f.ownerToken)
	if monitor.ActivationETag(latest) == monitor.ActivationETag(historical.Data) {
		t.Fatal("switch back reused historical tag")
	}
	t.Log("retained Schema8 maintenance protocol tested against guarded DB; no real physical execution or deployed client attestation")
}
