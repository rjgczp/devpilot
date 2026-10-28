package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type activationStub struct {
	repositoryStub
	value Activation
}

func (s *activationStub) ActivationDetail(ctx context.Context, _ session.Digest, _, _, _ string) (Activation, error) {
	s.result(ctx)
	return s.value, s.err
}
func (s *activationStub) ActivationUpdate(ctx context.Context, _ session.Digest, _, _, _ string, _ ActivationInput, _ string) (Activation, error) {
	s.result(ctx)
	return s.value, s.err
}
func activationStubValue() Activation {
	return Activation{WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, ConfigurationVersion: "1", DesiredState: "disabled", ActivationGeneration: "1", LastTransitionKind: "never_activated", CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC(), ExecutionState: "not_reported", BlockedReason: "execution_status_required"}
}
func TestActivationStrictProtocol(t *testing.T) {
	good := `{"desired_state":"enabled","configuration_version":"1"}`
	for _, body := range []string{`null`, `[]`, `{}`, `{"desired_state":null,"configuration_version":"1"}`, `{"desired_state":"enabled","configuration_version":1}`, good + ` {}`, `{"desired_state":"enabled","configuration_version":"1","desired_\u0073tate":"disabled"}`, `{"desired_state":"\ud800","configuration_version":"1"}`, `{"desired_state":"enabled","configuration_version":"1","actor":"x"}`} {
		if _, ok := parseActivation([]byte(body)); ok {
			t.Fatal("accepted body", body)
		}
	}
	for _, v := range []string{"0", "01", "+1", "-1", "1e2", "1.0", "9223372036854775808", " 1", ""} {
		if len(validateActivation(ActivationInput{"enabled", v})) == 0 {
			t.Fatal("version", v)
		}
	}
	for _, tag := range []string{"*", `W/` + ActivationETag(activationStubValue()), `"monitor:` + exampleID + `:1"`, strings.Repeat("x", 129), `"monitor-activation:` + exampleID + `:0:1"`, `"monitor-activation:` + exampleID + `:1:01"`, ActivationETag(activationStubValue()) + `, ` + ActivationETag(activationStubValue())} {
		if _, _, _, ok := activationCondition(tag); ok {
			t.Fatal("tag", tag)
		}
	}
	if _, _, _, ok := activationCondition(ActivationETag(activationStubValue())); !ok {
		t.Fatal("valid tag")
	}
}
func TestActivationTransport(t *testing.T) {
	good := `{"desired_state":"enabled","configuration_version":"1"}`
	for _, tc := range []struct {
		name, method, body, key, value string
		status                         int
		code                           string
		calls                          int32
	}{
		{name: "get", method: "GET", status: 200, calls: 1},
		{name: "put", method: "PUT", body: good, status: 200, calls: 1},
		{name: "csrf", method: "PUT", body: good, key: "Origin", value: "null", status: 403, code: "csrf_rejected"},
		{name: "cookie", method: "PUT", body: good, key: "Cookie", value: "", status: 401, code: "unauthenticated"},
		{name: "media", method: "PUT", body: good, key: "Content-Type", value: "text/plain", status: 415, code: "unsupported_media_type"},
		{name: "encoding", method: "PUT", body: good, key: "Content-Encoding", value: "identity", status: 415, code: "unsupported_media_type"},
		{name: "body", method: "PUT", body: good + "{}", status: 400, code: "invalid_request"},
		{name: "huge", method: "PUT", body: good + strings.Repeat(" ", 1025), status: 413, code: "request_too_large"},
		{name: "field", method: "PUT", body: `{"desired_state":"running","configuration_version":"1"}`, status: 422, code: "validation_failed"},
		{name: "weak", method: "PUT", body: good, key: "If-Match", value: "*", status: 400, code: "invalid_request"},
		{name: "conditional-get", method: "GET", key: "If-Match", value: ActivationETag(activationStubValue()), status: 400, code: "invalid_request"},
		{name: "get-body", method: "GET", body: " ", status: 400, code: "invalid_request"},
		{name: "head", method: "HEAD", status: 405, code: "method_not_allowed"},
		{name: "options", method: "OPTIONS", status: 405, code: "method_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &activationStub{value: activationStubValue()}
			h := testHandler(&repositoryStub{}, io.Discard)
			h.store = s
			r := request(tc.method, itemPath+"/activation", tc.body)
			if tc.method == "PUT" {
				r.Header.Set("If-Match", ActivationETag(s.value))
			}
			if tc.key != "" {
				r.Header.Set(tc.key, tc.value)
			}
			w := response(t, h, r, tc.status, tc.code)
			if s.calls.Load() != tc.calls {
				t.Fatal("unexpected DB calls")
			}
			if tc.status == 200 {
				if w.Header().Get("DevPilot-Activation-Projection") != "intent-v2" {
					t.Fatal("projection protocol")
				}
				if w.Header().Get("ETag") != ActivationETag(s.value) {
					t.Fatal("tag")
				}
				var v map[string]map[string]any
				if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v["data"]) != 14 {
					t.Fatal("projection", w.Body.String())
				}
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET, PUT" {
				t.Fatal("allow")
			}
		})
	}
	for _, suffix := range []string{"/activation?x=1", "//activation", "/activation/", "/activation/x"} {
		s := &activationStub{}
		h := testHandler(&repositoryStub{}, io.Discard)
		h.store = s
		r := request("GET", itemPath+suffix, "")
		status := 404
		if strings.Contains(suffix, "?") {
			status = 400
		}
		response(t, h, r, status, "")
		if s.calls.Load() != 0 {
			t.Fatal("alias called store")
		}
	}
}
func TestActivationErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{session.ErrInvalid, 401, "unauthenticated"}, {ErrNotFound, 404, "monitor_not_found"}, {ErrForbidden, 403, "monitor_forbidden"}, {ErrPreconditionRequired, 428, "precondition_required"}, {ErrPreconditionFailed, 412, "precondition_failed"}, {ErrActivationLimit, 409, "activation_limit_reached"}, {ActivationRateLimit{60}, 429, "activation_rate_limited"}, {user.ErrSchema, 500, "internal_error"}, {user.ErrTimeout, 503, "database_timeout"}, {user.ErrUnavailable, 503, "service_unavailable"}, {user.ErrCanceled, 500, "internal_error"}} {
		var logs bytes.Buffer
		s := &activationStub{repositoryStub: repositoryStub{err: tc.err}, value: activationStubValue()}
		h := testHandler(&repositoryStub{}, &logs)
		h.store = s
		r := request("PUT", itemPath+"/activation", `{"desired_state":"disabled","configuration_version":"1"}`)
		w := response(t, h, r, tc.status, tc.code)
		if tc.status == 429 && w.Header().Get("Retry-After") != "60" {
			t.Fatal("retry")
		}
		if strings.Contains(w.Body.String()+logs.String(), "private-path") {
			t.Fatal("secret")
		}
	}
	s := &activationStub{repositoryStub: repositoryStub{run: func(context.Context) { panic("PRIVATE") }}}
	h := testHandler(&repositoryStub{}, io.Discard)
	h.store = s
	response(t, h, request("GET", itemPath+"/activation", ""), 500, "internal_error")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := request("GET", itemPath+"/activation", "").WithContext(ctx)
	s.calls.Store(0)
	h.ServeHTTP(&discardResponse{}, r)
	if s.calls.Load() != 0 {
		t.Fatal("canceled DB")
	}
}

type discardResponse struct{}

func (*discardResponse) Header() http.Header       { return http.Header{} }
func (*discardResponse) Write([]byte) (int, error) { panic("unexpected canceled write") }
func (*discardResponse) WriteHeader(int)           { panic("unexpected canceled response") }
func TestActivationOpenAPI(t *testing.T) {
	b, e := os.ReadFile("../../docs/api/openapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		t.Fatal("OpenAPI JSON")
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["MonitorActivation"].(map[string]any)["properties"].(map[string]any)
	if len(props) != 14 {
		t.Fatal("documented projection")
	}
	for _, forbidden := range []string{"url", "actor", "last_actor_user_id", "health", "run_id", "lease_until"} {
		if _, ok := props[forbidden]; ok {
			t.Fatal("private field")
		}
	}
	for _, method := range []string{"GET", "PUT"} {
		for _, tc := range []struct {
			err    error
			status int
		}{{nil, 200}, {nil, 422}, {session.ErrInvalid, 401}, {ErrNotFound, 404}, {user.ErrSchema, 500}, {user.ErrUnavailable, 503}, {ErrForbidden, 403}, {ErrPreconditionRequired, 428}, {ErrPreconditionFailed, 412}, {ErrActivationLimit, 409}, {ActivationRateLimit{60}, 429}} {
			if method == "GET" && (tc.status == 422 || tc.status == 403 || tc.status == 428 || tc.status == 412 || tc.status == 409 || tc.status == 429) {
				continue
			}
			s := &activationStub{repositoryStub: repositoryStub{err: tc.err}, value: activationStubValue()}
			h := testHandler(&repositoryStub{}, io.Discard)
			h.store = s
			body := ""
			if method == "PUT" {
				body = `{"desired_state":"enabled","configuration_version":"1"}`
			}
			if tc.status == 422 {
				body = `{"desired_state":"running","configuration_version":"0"}`
			}
			r := request(method, itemPath+"/activation", body)
			w := response(t, h, r, tc.status, "")
			route := "/api/v1/workspaces/{workspace_id}/projects/{project_id}/monitors/{monitor_id}/activation"
			rs := doc["paths"].(map[string]any)[route].(map[string]any)[strings.ToLower(method)].(map[string]any)["responses"].(map[string]any)[strconv.Itoa(tc.status)].(map[string]any)
			var actual any
			if json.Unmarshal(w.Body.Bytes(), &actual) != nil {
				t.Fatal("response")
			}
			schema := rs["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
			if !contractOK(doc, schema, actual) {
				t.Fatal("OpenAPI response mismatch", method, tc.status)
			}
		}
	}
}
func TestActivationTransportEdgesAndDeadline(t *testing.T) {
	for _, kind := range []string{"repeated-tag", "repeated-media", "other-condition", "raw-path", "cross-site", "query-marker", "deadline", "cancel-after-store"} {
		t.Run(kind, func(t *testing.T) {
			s := &activationStub{value: activationStubValue()}
			h := testHandler(&repositoryStub{}, io.Discard)
			h.store = s
			r := request("PUT", itemPath+"/activation", `{"desired_state":"disabled","configuration_version":"1"}`)
			r.Header.Set("If-Match", ActivationETag(s.value))
			status := 400
			switch kind {
			case "repeated-tag":
				r.Header.Add("If-Match", ActivationETag(s.value))
			case "repeated-media":
				r.Header.Add("Content-Type", "application/json")
				status = 415
			case "other-condition":
				r.Header.Set("If-None-Match", "*")
			case "raw-path":
				r.URL.RawPath = r.URL.Path
				status = 404
			case "cross-site":
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				status = 403
			case "query-marker":
				r.URL.ForceQuery = true
			case "deadline":
				h.databaseTimeout = 10 * time.Millisecond
				s.run = func(ctx context.Context) { <-ctx.Done() }
				status = 503
			case "cancel-after-store":
				ctx, cancel := context.WithCancel(r.Context())
				r = r.WithContext(ctx)
				s.run = func(context.Context) { cancel() }
				h.ServeHTTP(&discardResponse{}, r)
				return
			}
			response(t, h, r, status, "")
			if kind != "deadline" && s.calls.Load() != 0 {
				t.Fatal("early denial accessed DB")
			}
		})
	}
}
