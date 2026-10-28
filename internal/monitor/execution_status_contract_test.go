package monitor

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecutionStatusPopulatedOpenAPIContract(t *testing.T) {
	raw, e := os.ReadFile("../../docs/api/openapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("OpenAPI JSON")
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	schema := schemas["MonitorExecutionStatusResponse"].(map[string]any)
	utc := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	code, duration, success := 200, 1, true
	for _, state := range []string{"leased", "started", "completed", "canceled", "expired", "unknown"} {
		t.Run(state, func(t *testing.T) {
			attempt := &Attempt{WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, RunID: exampleID, ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "3", FencingToken: "4", AttemptState: state, ScheduledAt: utc, ClaimedAt: utc}
			if state == "started" || state == "completed" || state == "unknown" {
				attempt.StartedAt = &utc
			}
			if state != "leased" && state != "started" {
				attempt.FinishedAt = &utc
			}
			status := ExecutionStatus{WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: &attempt.SchedulerEpoch, DesiredState: "enabled", AuthorizationState: "valid", SchedulingState: "authorized_waiting", EffectiveAuthorization: true, ExpiresAt: &utc, PauseSemantics: "start_commit", LatestAttempt: attempt}
			if state == "completed" {
				attempt.HTTPStatus = &code
				attempt.DurationMS = &duration
				attempt.Success = &success
				status.CurrentObservation = attempt
				status.LatestCompletedObservation = attempt
			}
			s := &executionStub{status: status}
			h := testHandler(&repositoryStub{}, io.Discard)
			h.store = s
			w := response(t, h, request("GET", itemPath+"/execution-status", ""), 200, "")
			var actual map[string]any
			if json.Unmarshal(w.Body.Bytes(), &actual) != nil || !contractOK(doc, schema, actual) {
				t.Fatal("populated OpenAPI response", w.Body.String())
			}
			a := actual["data"].(map[string]any)["latest_attempt"].(map[string]any)
			for _, secret := range []string{"url", "actor", "lease_owner", "worker_incarnation", "ip", "origin_key", "raw_error"} {
				a[secret] = "SENTINEL"
				if contractOK(doc, schema, actual) {
					t.Fatal("private field admitted", secret)
				}
				delete(a, secret)
			}
			if state == "unknown" && (a["success"] != nil || a["http_status"] != nil || a["failure_code"] != nil) {
				t.Fatal("unknown invented HTTP outcome")
			}
			a["attempt_state"] = "healthy"
			if contractOK(doc, schema, actual) {
				t.Fatal("invented total state admitted")
			}
		})
	}
	for _, name := range []string{"latest_attempt", "latest_completed_observation", "current_observation"} {
		p := schemas["MonitorExecutionStatus"].(map[string]any)["properties"].(map[string]any)[name].(map[string]any)
		if contractOK(doc, p, []any{}) {
			t.Fatal("history collection admitted", name)
		}
	}
}

func TestExecutionStatusOrthogonalSchema(t *testing.T) {
	raw, e := os.ReadFile("../../docs/api/openapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("OpenAPI JSON")
	}
	props := doc["components"].(map[string]any)["schemas"].(map[string]any)["MonitorExecutionStatus"].(map[string]any)["properties"].(map[string]any)
	for key, want := range map[string]string{"authorization_state": "none,valid,expired", "scheduling_state": "disabled,authorized_waiting,due,blocked"} {
		choices := props[key].(map[string]any)["enum"].([]any)
		var names []string
		for _, v := range choices {
			names = append(names, v.(string))
		}
		if strings.Join(names, ",") != want {
			t.Fatal("mixed execution dimensions", key)
		}
	}
}
