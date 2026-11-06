//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
)

type status75Pair struct {
	a                  monitor.Activation
	s                  monitor.ExecutionStatus
	tag                string
	activation, status []byte
}

func status75Base(m monitor.Public) string {
	return "/api/v1/workspaces/" + m.WorkspaceID + "/projects/" + m.ProjectID + "/monitors/" + m.ID
}
func status75Status(t *testing.T, c *http.Client, server string, m monitor.Public, token string) (monitor.ExecutionStatus, []byte) {
	t.Helper()
	code, h, raw := checkpointHTTP(t, c, server, "GET", status75Path(m), "", token, "", nil)
	if code != 200 || h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("ETag") != "" {
		t.Fatal("authoritative HTTP response", code, string(raw))
	}
	return status74Decode(t, raw), raw
}
func status75Path(m monitor.Public) string { return status75Base(m) + "/execution-status" }
func status75ReadPair(t *testing.T, c *http.Client, server string, m monitor.Public, token string) status75Pair {
	t.Helper()
	a, h, raw := checkpointProjectionGet(t, c, server, status75Base(m), token)
	s, status := status75Status(t, c, server, m, token)
	if a.ConfigurationVersion != s.ConfigurationVersion || a.ActivationGeneration != s.ActivationGeneration || a.DesiredState != s.DesiredState || a.WorkspaceID != s.WorkspaceID || a.ProjectID != s.ProjectID || a.MonitorID != s.MonitorID {
		t.Fatal("mixed quiet-window pair", a, s)
	}
	if h.Get("ETag") != monitor.ActivationETag(a) || checkpointClientObservation("intent-v2", a, 200, status) == "refresh_status" {
		t.Fatal("quiet-window consumer rejected matching lineage")
	}
	t.Logf("pair v=%s g=%s intent=%s authorization=%s scheduling=%s in_flight=%t", s.ConfigurationVersion, s.ActivationGeneration, s.DesiredState, s.AuthorizationState, s.SchedulingState, s.InFlight)
	return status75Pair{a: a, s: s, tag: h.Get("ETag"), activation: raw, status: status}
}
func status75Authorize(t *testing.T, c *http.Client, server string, m monitor.Public, token string) {
	t.Helper()
	a, h, _ := checkpointProjectionGet(t, c, server, status75Base(m), token)
	body := fmt.Sprintf(`{"configuration_version":%q,"consent_profile":"periodic-http-get-v1","authorization_seconds":60}`, a.ConfigurationVersion)
	code, _, raw := checkpointHTTP(t, c, server, "PUT", status75Base(m)+"/execution-authorization", body, token, h.Get("ETag"), nil)
	if code != 200 {
		t.Fatal("Owner HTTP authorization", code, string(raw))
	}
}
func status75Gate(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	t.Cleanup(func() {
		// Offline projection setup has no Run or physical capability. Close
		// only its test deployment gate before the guarded Goose teardown.
		if _, e := db.Exec(context.Background(), `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
			t.Error(e)
		}
	})
	// Projection input only: no Worker/Probe exists in these offline HTTP tests.
	if _, e := db.Exec(context.Background(), `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
}
func TestExecutionStatusCrossEndpointMatrix(t *testing.T) {
	db := workspaceDB(t)
	server, c, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	for _, state := range []string{"disabled", "authorization_required", "scheduler_unavailable", "authorized_waiting", "due", "configuration_invalidation"} {
		t.Run(state, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			ctx := context.Background()
			if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
				t.Fatal(e)
			}
			if state == "authorization_required" {
				a := activationRead(t, db, f.od, m)
				code, _, raw := checkpointHTTP(t, c, server.URL, "PUT", status75Base(m)+"/activation", `{"desired_state":"enabled","configuration_version":"1"}`, f.ownerToken, monitor.ActivationETag(a), nil)
				if code != 200 {
					t.Fatal(code, string(raw))
				}
			} else if state != "disabled" {
				status75Authorize(t, c, server.URL, m, f.ownerToken)
			}
			if state == "authorized_waiting" || state == "due" {
				status75Gate(t, db)
				offset := "1 hour"
				if state == "due" {
					offset = "-1 second"
				}
				if _, e := db.Exec(ctx, `UPDATE monitor_schedules SET next_due_at=clock_timestamp()+$4::interval WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, m.WorkspaceID, m.ProjectID, m.ID, offset); e != nil {
					t.Fatal(e)
				}
			}
			if state == "configuration_invalidation" {
				code, _, raw := checkpointHTTP(t, c, server.URL, "PATCH", status75Base(m), `{"name":"changed configuration"}`, f.ownerToken, monitor.ETag(m), nil)
				if code != 200 {
					t.Fatal(code, string(raw))
				}
			}
			for _, token := range []string{f.ownerToken, f.memberToken} {
				before := status75ReadPair(t, c, server.URL, m, token)
				want := "blocked"
				if state == "disabled" || state == "configuration_invalidation" {
					want = "disabled"
				}
				if state == "due" || state == "authorized_waiting" {
					want = state
				}
				if before.s.SchedulingState != want || before.s.LatestAttempt != nil || before.s.CurrentObservation != nil || before.s.InFlight {
					t.Fatal(state, before.s)
				}
				if state == "authorization_required" || state == "scheduler_unavailable" {
					if before.s.BlockedReason == nil || *before.s.BlockedReason != state || before.s.EffectiveAuthorization {
						t.Fatal("wrong blocking fact", state, before.s)
					}
				}
				if state == "configuration_invalidation" && (before.a.ConfigurationVersion != "2" || before.s.AuthorizationState != "none" || before.a.DesiredState != "disabled") {
					t.Fatal("PATCH did not revoke", before.s)
				}
				after := status75ReadPair(t, c, server.URL, m, token)
				if !bytes.Equal(before.activation, after.activation) || before.tag != after.tag {
					t.Fatal("reads mutated Activation")
				}
				for _, code := range []int{0, 404, 503} {
					if checkpointClientObservation("intent-v2", before.a, code, before.status) != "运行状态不可用" {
						t.Fatal("unavailable status inferred execution")
					}
				}
			}
		})
	}
}

// A real parent-row barrier surrounds the actual HTTP mutation. Reads before
// writer commit see old facts; reads after commit see complete new facts.
func status75MutationBarrier(t *testing.T, db *pgxpool.Pool, f memberFixture, m monitor.Public, operation, order string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	pid := make(chan int, 1)
	origin, e := config.ParseOrigin("https://app.example.com")
	if e != nil {
		t.Fatal(e)
	}
	store := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}
	server, c := checkpointHTTPHandlerServer(t, monitor.New(store, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	defer server.Close()
	old := status75ReadPair(t, c, server.URL, m, f.memberToken)
	method, path, body, tag := "PATCH", status75Base(m), `{"name":"committed between reads"}`, monitor.ETag(m)
	if operation == "pause" {
		method = "PUT"
		path += "/activation"
		body = fmt.Sprintf(`{"desired_state":"disabled","configuration_version":%q}`, old.a.ConfigurationVersion)
		tag = old.tag
	}
	if operation == "authorize" {
		method = "PUT"
		path += "/execution-authorization"
		body = fmt.Sprintf(`{"configuration_version":%q,"consent_profile":"periodic-http-get-v1","authorization_seconds":60}`, old.a.ConfigurationVersion)
		tag = old.tag
	}
	blocker, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var blockerPID int
	if e = blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); e != nil {
		t.Fatal(e)
	}
	if _, e = blocker.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, m.WorkspaceID); e != nil {
		t.Fatal(e)
	}
	done := make(chan int, 1)
	go func() {
		code, _, _ := checkpointHTTP(t, c, server.URL, method, path, body, f.ownerToken, tag, nil)
		done <- code
	}()
	var writerPID int
	select {
	case writerPID = <-pid:
	case code := <-done:
		t.Fatal("HTTP writer returned before barrier", code)
	case <-ctx.Done():
		t.Fatal("PID barrier", ctx.Err())
	}
	waitActivationBlock(t, db, writerPID)
	var locks []byte
	if e = db.QueryRow(ctx, `SELECT jsonb_build_object('writer',$1::int,'barrier',$2::int,'blocking_pids',pg_blocking_pids($1),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l WHERE pid IN ($1,$2)))`, writerPID, blockerPID).Scan(&locks); e != nil {
		t.Fatal(e)
	}
	// The first endpoint is read while the actual mutation is proven blocked.
	held := status75ReadPair(t, c, server.URL, m, f.memberToken)
	if held.tag != old.tag {
		t.Fatal("uncommitted mutation leaked")
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case code := <-done:
		if code != 200 {
			t.Fatal("mutation", code)
		}
	case <-ctx.Done():
		t.Fatal("writer completion", ctx.Err())
	}
	now := status75ReadPair(t, c, server.URL, m, f.memberToken)
	if now.a.ActivationGeneration == old.a.ActivationGeneration {
		t.Fatal("generation did not change")
	}
	if operation == "patch" && now.a.ConfigurationVersion == old.a.ConfigurationVersion {
		t.Fatal("version did not change")
	}
	if operation != "patch" && now.a.ConfigurationVersion != old.a.ConfigurationVersion {
		t.Fatal("intent command changed configuration version")
	}
	staleA, staleStatus := old.a, now.status
	if order == "status-first" {
		staleA, staleStatus = now.a, old.status
	}
	if checkpointClientObservation("intent-v2", staleA, 200, staleStatus) != "refresh_status" {
		t.Fatal("consumer joined incompatible snapshots", operation, order)
	}
	// Refreshing authority and the intent precondition yields one coherent pair.
	refreshed := status75ReadPair(t, c, server.URL, m, f.memberToken)
	if refreshed.tag != now.tag || checkpointClientObservation("intent-v2", now.a, 200, refreshed.status) == "refresh_status" {
		t.Fatal("consumer refresh failed")
	}
	t.Log(operation, order, "waiting lock graph", string(locks), "commit separates g", old.a.ActivationGeneration, now.a.ActivationGeneration)
}
func TestExecutionStatusCrossEndpointSnapshots(t *testing.T) {
	db := workspaceDB(t)
	for _, operation := range []string{"patch", "pause", "authorize"} {
		for _, order := range []string{"activation-first", "status-first"} {
			t.Run(operation+"/"+order, func(t *testing.T) {
				f, m := monitorFixture(t, db)
				status75MutationBarrier(t, db, f, m, operation, order)
			})
		}
	}
}
func TestExecutionStatusCrossEndpointExpiry(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	server, c, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	status75Gate(t, db)
	status75Authorize(t, c, server.URL, m, f.ownerToken)
	before := status75ReadPair(t, c, server.URL, m, f.memberToken)
	if before.s.ExpiresAt == nil || before.s.AuthorizationState != "valid" {
		t.Fatal("missing real expiry")
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
	for _, token := range []string{f.ownerToken, f.memberToken} {
		after := status75ReadPair(t, c, server.URL, m, token)
		if after.s.AuthorizationState != "expired" || after.s.SchedulingState != "blocked" || after.s.EffectiveAuthorization || after.s.BlockedReason == nil || *after.s.BlockedReason != "authorization_expired" || !bytes.Equal(before.activation, after.activation) || before.tag != after.tag {
			t.Fatal("expiry changed intent or concealed authority", after.s)
		}
	}
	server.Close()
	// Real sixty-second cooldown has also elapsed; same-version renewal changes g.
	status75MutationBarrier(t, db, f, m, "authorize", "activation-first")
}
func TestExecutionStatusCrossEndpointAuthority(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	server, c, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	for _, suffix := range []string{"/activation", "/execution-status"} {
		for _, tc := range []struct {
			token string
			code  int
		}{{f.ownerToken, 200}, {f.memberToken, 200}, {"", 401}, {f.otherToken, 404}} {
			code, _, raw := checkpointHTTP(t, c, server.URL, "GET", status75Base(m)+suffix, "", tc.token, "", nil)
			if code != tc.code {
				t.Fatal("paired authority", suffix, code, tc.code)
			}
			if code != 200 {
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				want := "monitor_not_found"
				if code == 401 {
					want = "unauthenticated"
				}
				if json.Unmarshal(raw, &body) != nil || body.Error.Code != want || bytes.Contains(raw, []byte(`"data"`)) || bytes.Contains(raw, []byte(m.ID)) {
					t.Fatal("error disclosed scope", string(raw))
				}
			}
		}
	}
	name := "other project"
	otherProject, e := (project.Store{Pool: db}).Create(context.Background(), f.xd, f.z.ID, project.Input{Name: &name})
	if e != nil {
		t.Fatal(e)
	}
	other, e := (monitor.Store{Pool: db}).Create(context.Background(), f.xd, f.z.ID, otherProject.ID, monitorInput("other monitor"))
	if e != nil {
		t.Fatal(e)
	}
	// The existing Member genuinely belongs to both Workspaces.
	for _, suffix := range []string{"/activation", "/execution-status"} {
		for _, mixed := range []monitor.Summary{{WorkspaceID: m.WorkspaceID, ProjectID: other.ProjectID, ID: other.ID}, {WorkspaceID: other.WorkspaceID, ProjectID: m.ProjectID, ID: m.ID}, {WorkspaceID: m.WorkspaceID, ProjectID: m.ProjectID, ID: other.ID}} {
			code, _, raw := checkpointHTTP(t, c, server.URL, "GET", status75Base(monitor.Public{Summary: mixed})+suffix, "", f.memberToken, "", nil)
			if code != 404 || !bytes.Contains(raw, []byte(`"code":"monitor_not_found"`)) || bytes.Contains(raw, []byte(`"data"`)) {
				t.Fatal("mixed valid scope disclosed data", code, string(raw))
			}
		}
	}
	if _, e := db.Exec(context.Background(), `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, m.WorkspaceID, f.member.ID); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"/activation", "/execution-status"} {
		code, _, raw := checkpointHTTP(t, c, server.URL, "GET", status75Base(m)+suffix, "", f.memberToken, "", nil)
		if code != 404 || bytes.Contains(raw, []byte(`"data"`)) {
			t.Fatal("removed Member retained authority", code)
		}
	}
}
