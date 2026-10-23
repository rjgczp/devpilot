package monitor

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
)

type executionStub struct {
	repositoryStub
	authorization ExecutionAuthorization
	status        ExecutionStatus
}

func (s *executionStub) ExecutionAuthorize(ctx context.Context, _ session.Digest, _, _, _ string, _ ExecutionAuthorizationInput, _ string) (ExecutionAuthorization, error) {
	s.result(ctx)
	return s.authorization, s.err
}
func (s *executionStub) ExecutionStatus(ctx context.Context, _ session.Digest, _, _, _ string) (ExecutionStatus, error) {
	s.result(ctx)
	return s.status, s.err
}

const authorizationBody = `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":60}`

func TestExecutionAuthorizationStrictBody(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{}`, authorizationBody + ` {}`, `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":null}`, `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":"60"}`, `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":60.0}`, `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":6e1}`, strings.Replace(authorizationBody, `"1"`, `1`, 1), strings.Replace(authorizationBody, `"1"`, `"\ud800"`, 1), strings.TrimSuffix(authorizationBody, "}") + `,"configuration_\u0076ersion":"2"}`, strings.TrimSuffix(authorizationBody, "}") + `,"extra":true}`, string(append([]byte(authorizationBody[:len(authorizationBody)-1]), 0xff, '}'))} {
		if _, ok := parseExecutionAuthorization([]byte(body)); ok {
			t.Fatal("accepted invalid body")
		}
	}
	for _, version := range []string{"0", "01", "+1", "-1", "1e2", "1.0", "9223372036854775808", " 1", ""} {
		a, ok := parseExecutionAuthorization([]byte(strings.Replace(authorizationBody, `"1"`, `"`+version+`"`, 1)))
		if !ok || validateExecutionAuthorization(a)["configuration_version"] != "invalid_value" {
			t.Fatal("version", version)
		}
	}
	for _, seconds := range []int{59, 86401, -1} {
		if validateExecutionAuthorization(ExecutionAuthorizationInput{"1", "periodic-http-get-v1", seconds})["authorization_seconds"] != "invalid_value" {
			t.Fatal("seconds")
		}
	}
	if a, ok := parseExecutionAuthorization([]byte(authorizationBody)); !ok || len(validateExecutionAuthorization(a)) != 0 {
		t.Fatal("valid body rejected")
	}
}

func TestExecutionHTTPTransport(t *testing.T) {
	for _, tc := range []struct {
		name, resource, method, body, key, value string
		status                                   int
		calls                                    int32
	}{
		{"grant", "execution-authorization", "PUT", authorizationBody, "", "", 200, 1},
		{"status", "execution-status", "GET", "", "", "", 200, 1},
		{"cookie", "execution-status", "GET", "", "Cookie", "", 401, 0},
		{"csrf", "execution-authorization", "PUT", authorizationBody, "Origin", "null", 403, 0},
		{"media", "execution-authorization", "PUT", authorizationBody, "Content-Type", "text/plain", 415, 0},
		{"encoding", "execution-authorization", "PUT", authorizationBody, "Content-Encoding", "identity", 415, 0},
		{"duplicate", "execution-authorization", "PUT", strings.TrimSuffix(authorizationBody, "}") + `,"authorization_seconds":60}`, "", "", 400, 0},
		{"oversized", "execution-authorization", "PUT", authorizationBody + strings.Repeat(" ", 1025), "", "", 413, 0},
		{"edge1024", "execution-authorization", "PUT", authorizationBody + strings.Repeat(" ", 1024-len(authorizationBody)), "", "", 200, 1},
		{"weak", "execution-authorization", "PUT", authorizationBody, "If-Match", `W/` + ActivationETag(activationStubValue()), 400, 0},
		{"wildcard", "execution-authorization", "PUT", authorizationBody, "If-Match", "*", 400, 0},
		{"status-no-conditional", "execution-status", "GET", "", "If-Match", ActivationETag(activationStubValue()), 400, 0},
		{"status-body", "execution-status", "GET", " ", "", "", 400, 0},
		{"status-put", "execution-status", "PUT", "", "", "", 405, 0},
		{"grant-get", "execution-authorization", "GET", "", "", "", 405, 0},
		{"status-head", "execution-status", "HEAD", "", "", "", 405, 0},
		{"grant-options", "execution-authorization", "OPTIONS", "", "", "", 405, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &executionStub{authorization: ExecutionAuthorization{WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "1", IssuedAt: time.Unix(0, 0).UTC(), ExpiresAt: time.Unix(60, 0).UTC()}, status: ExecutionStatus{DesiredState: "disabled", WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, ConfigurationVersion: "1", ActivationGeneration: "1", AuthorizationState: "none", SchedulingState: "disabled", PauseSemantics: "start_commit"}}
			h := testHandler(&repositoryStub{}, io.Discard)
			h.store = s
			r := request(tc.method, itemPath+"/"+tc.resource, tc.body)
			if tc.method == "PUT" {
				r.Header.Set("If-Match", ActivationETag(activationStubValue()))
			}
			if tc.key != "" {
				r.Header.Set(tc.key, tc.value)
			}
			w := response(t, h, r, tc.status, "")
			if s.calls.Load() != tc.calls {
				t.Fatal("unexpected DB call")
			}
			if tc.status == 200 {
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("response protection")
				}
				var v map[string]any
				if json.Unmarshal(w.Body.Bytes(), &v) != nil {
					t.Fatal("JSON")
				}
			}
			if tc.status == 405 {
				want := "GET"
				if tc.resource == "execution-authorization" {
					want = "PUT"
				}
				if w.Header().Get("Allow") != want {
					t.Fatal("allow")
				}
			}
		})
	}
	for _, key := range []string{"If-Match", "Content-Type"} {
		s := &executionStub{}
		h := testHandler(&repositoryStub{}, io.Discard)
		h.store = s
		r := request("PUT", itemPath+"/execution-authorization", authorizationBody)
		if key == "If-Match" {
			r.Header.Set(key, ActivationETag(activationStubValue()))
		}
		r.Header.Add(key, r.Header.Get(key))
		want := 400
		if key == "Content-Type" {
			want = 415
		}
		response(t, h, r, want, "")
		if s.calls.Load() != 0 {
			t.Fatal("duplicate header reached DB")
		}
	}
}

func TestExecutionOpenAPIResponses(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("invalid document")
	}
	s := &executionStub{authorization: ExecutionAuthorization{WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "1", IssuedAt: time.Unix(0, 0).UTC(), ExpiresAt: time.Unix(60, 0).UTC()}, status: ExecutionStatus{DesiredState: "disabled", WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, ConfigurationVersion: "1", ActivationGeneration: "1", AuthorizationState: "none", SchedulingState: "disabled", PauseSemantics: "start_commit"}}
	for _, resource := range []string{"execution-authorization", "execution-status"} {
		method, body := "GET", ""
		if resource == "execution-authorization" {
			method = "PUT"
			body = authorizationBody
		}
		h := testHandler(&repositoryStub{}, io.Discard)
		h.store = s
		r := request(method, itemPath+"/"+resource, body)
		w := response(t, h, r, 200, "")
		var actual any
		if json.Unmarshal(w.Body.Bytes(), &actual) != nil {
			t.Fatal("invalid JSON")
		}
		route := "/api/v1/workspaces/{workspace_id}/projects/{project_id}/monitors/{monitor_id}/" + resource
		schema := doc["paths"].(map[string]any)[route].(map[string]any)[strings.ToLower(method)].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if !contractOK(doc, schema, actual) {
			t.Fatal("response contract", resource)
		}
		actual.(map[string]any)["data"].(map[string]any)["URL"] = "sentinel"
		if contractOK(doc, schema, actual) {
			t.Fatal("sensitive field accepted")
		}
	}
}
