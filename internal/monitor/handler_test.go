package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

const exampleID = "019a5abc-1234-7123-8123-123456789abc"

var itemPath = "/api/v1/workspaces/" + exampleID + "/projects/" + exampleID + "/monitors/" + exampleID
var listPath = "/api/v1/workspaces/" + exampleID + "/projects/" + exampleID + "/monitors"

type repositoryStub struct {
	calls atomic.Int32
	err   error
	run   func(context.Context)
	input Input
	tag   string
}

func (s *repositoryStub) result(ctx context.Context) (Public, error) {
	s.calls.Add(1)
	if s.run != nil {
		s.run(ctx)
	}
	return Public{Summary: Summary{ID: exampleID, WorkspaceID: exampleID, ProjectID: exampleID, Name: "团队", Method: "GET", IntervalSeconds: 60, TimeoutSeconds: 10, ExpectedStatus: 200, Version: "1", CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}, URL: "https://example.com/private-path"}, s.err
}
func (s *repositoryStub) Create(ctx context.Context, _ session.Digest, _, _ string, v Input) (Public, error) {
	s.input = v
	return s.result(ctx)
}
func (s *repositoryStub) Update(ctx context.Context, _ session.Digest, _, _, _ string, v Input, tag string) (Public, error) {
	s.input = v
	s.tag = tag
	return s.result(ctx)
}
func (s *repositoryStub) Detail(ctx context.Context, _ session.Digest, _, _, _ string) (Public, error) {
	return s.result(ctx)
}
func (s *repositoryStub) List(ctx context.Context, _ session.Digest, _, _ string, _ int, _ string) (Page, error) {
	_, err := s.result(ctx)
	return Page{Data: []Summary{}}, err
}
func testHandler(s *repositoryStub, out io.Writer) *Handler {
	origin, _ := config.ParseOrigin("https://app.example.com")
	return New(s, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(out, nil)))
}
func request(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	r.Header.Set("Cookie", "__Host-devpilot_session="+strings.Repeat("A", 43))
	if method == "PATCH" {
		r.Header.Set("If-Match", ETag(Public{Summary: Summary{ID: exampleID, Version: "1"}}))
	}
	return r
}
func response(t *testing.T, h *Handler, r *http.Request, status int, code string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("got %d want%d: %s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("headers")
	}
	if status >= 400 && (w.Header().Get("ETag") != "" || w.Header().Get("Location") != "") {
		t.Fatal("success metadata on error")
	}
	if r.Method == "HEAD" {
		if w.Body.Len() != 0 {
			t.Fatal("head body")
		}
		return w
	}
	if code != "" {
		var out struct {
			Error struct{ Code, Message string }
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Error.Code != code || out.Error.Message != messages[code] {
			t.Fatal("error contract")
		}
	}
	return w
}
func TestMonitorInputProtocol(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"name":"x"}`, 400, "invalid_request"}, {`{"name":"x","url":null}`, 400, "invalid_request"},
		{`{"name":"x","url":"https://example.com/?token=PRIVATE"}`, 422, "validation_failed"},
		{`{"name":"x","url":"https://example.com/","timeout_seconds":1e1}`, 400, "invalid_request"},
		{`{"name":"x","url":"https://example.com/"}` + strings.Repeat(" ", MaxBodyBytes), 413, "request_too_large"},
	} {
		s := &repositoryStub{}
		response(t, testHandler(s, io.Discard), request("POST", listPath, tc.body), tc.status, tc.code)
		if s.calls.Load() != 0 {
			t.Fatal("invalid reached DB")
		}
	}
	s := &repositoryStub{}
	response(t, testHandler(s, io.Discard), request("POST", listPath, `{"name":" \t中文 ","url":"HTTP://EXAMPLE.COM.:80"}`), 201, "")
	if *s.input.Name != "中文" || *s.input.URL != "http://example.com/" || *s.input.IntervalSeconds != 60 {
		t.Fatal("normalization")
	}
	response(t, testHandler(s, io.Discard), request("PATCH", itemPath, `{"name":"x"}`), 200, "")
	if s.input.URL != nil || s.input.IntervalSeconds != nil {
		t.Fatal("patch defaults")
	}
	for _, field := range []string{"id", "workspace_id", "project_id", "created_by_user_id", "version", "enabled", "headers", "proxy", "redirect"} {
		response(t, testHandler(&repositoryStub{}, io.Discard), request("POST", listPath, `{"name":"x","url":"https://example.com/","`+field+`":1}`), 400, "invalid_request")
	}
}
func TestMonitorMethodCSRFSelectorsAndConditions(t *testing.T) {
	for _, route := range []string{listPath, itemPath} {
		for _, method := range []string{"HEAD", "OPTIONS", "PUT", "DELETE"} {
			s := &repositoryStub{}
			w := response(t, testHandler(s, io.Discard), request(method, route, ""), 405, "method_not_allowed")
			allow := "GET, POST"
			if route == itemPath {
				allow = "GET, PATCH"
			}
			if w.Header().Get("Allow") != allow || s.calls.Load() != 0 {
				t.Fatal("method")
			}
		}
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Set("Origin", "null") }, func(r *http.Request) { r.Header.Add("Origin", "https://app.example.com") }, func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com.evil.test") }, func(r *http.Request) { r.Header.Del("X-DevPilot-CSRF") }, func(r *http.Request) { r.Header.Add("X-DevPilot-CSRF", "1") }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, func(r *http.Request) {
		r.Header.Del("Origin")
		r.Header.Set("Referer", "https://app.example.com")
		r.Host = "app.example.com"
	}} {
		for _, m := range []string{"POST", "PATCH"} {
			s := &repositoryStub{}
			route := listPath
			if m == "PATCH" {
				route = itemPath
			}
			r := request(m, route, `{"name":"x","url":"https://example.com/"}`)
			mutate(r)
			response(t, testHandler(s, io.Discard), r, 403, "csrf_rejected")
			if s.calls.Load() != 0 {
				t.Fatal("csrf DB")
			}
		}
	}
	for _, cookie := range []string{"", "__Host-devpilot_session=bad", "devpilot_session=" + strings.Repeat("A", 43), "__Host-devpilot_session=" + strings.Repeat("A", 43) + "; __Host-devpilot_session=" + strings.Repeat("A", 43)} {
		r := request("GET", listPath, "")
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("A", 43))
		response(t, testHandler(&repositoryStub{}, io.Discard), r, 401, "unauthenticated")
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=01", "limit=+1", "limit=1&limit=2", "after=", "after=BAD", "foo=x", "x=%zz"} {
		response(t, testHandler(&repositoryStub{}, io.Discard), request("GET", listPath+"?"+q, ""), 400, "invalid_request")
	}
	for _, tag := range []string{"", "*", `W/"monitor:` + exampleID + `:1"`, `"monitor:` + exampleID + `:01"`, `"monitor:` + exampleID + `:9223372036854775808"`, `"monitor:` + exampleID + `:1", "x"`, strings.Repeat("x", 97)} {
		r := request("PATCH", itemPath, `{"name":"x","url":"https://example.com/"}`)
		r.Header.Set("If-Match", tag)
		response(t, testHandler(&repositoryStub{}, io.Discard), r, 400, "invalid_request")
	}
	r := request("PATCH", itemPath, `{"name":"x","url":"https://example.com/"}`)
	r.Header.Add("If-Match", r.Header.Get("If-Match"))
	response(t, testHandler(&repositoryStub{}, io.Discard), r, 400, "invalid_request")
	for _, header := range []string{"If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range"} {
		r := request("GET", itemPath, "")
		r.Header.Set(header, "x")
		response(t, testHandler(&repositoryStub{}, io.Discard), r, 400, "invalid_request")
	}
	for _, media := range []string{"text/plain", "application/json; charset=latin1", "application/json; x=y", "application/json; charset=utf-8; charset=utf-8"} {
		r := request("POST", listPath, `{"name":"x","url":"https://example.com/"}`)
		r.Header.Set("Content-Type", media)
		response(t, testHandler(&repositoryStub{}, io.Discard), r, 415, "unsupported_media_type")
	}
}
func TestMonitorFailuresCancellationAndPanic(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{session.ErrInvalid, 401, "unauthenticated"}, {ErrNotFound, 404, "monitor_not_found"}, {ErrForbidden, 403, "monitor_forbidden"}, {ErrPreconditionRequired, 428, "precondition_required"}, {ErrPreconditionFailed, 412, "precondition_failed"}, {user.ErrUnavailable, 503, "service_unavailable"}, {user.ErrTimeout, 503, "database_timeout"}, {user.ErrSchema, 500, "internal_error"}, {user.ErrCanceled, 500, "internal_error"}, {errors.New("PRIVATE SQL DSN email PHC digest token id If-Match"), 500, "internal_error"}} {
		for _, m := range []string{"GET", "POST", "PATCH"} {
			s := &repositoryStub{err: tc.err}
			var logs bytes.Buffer
			route := itemPath
			if m == "POST" {
				route = listPath
			}
			w := response(t, testHandler(s, &logs), request(m, route, `{"name":"x","url":"https://example.com/"}`), tc.status, tc.code)
			if s.calls.Load() != 1 || strings.Contains(logs.String()+w.Body.String(), "PRIVATE") {
				t.Fatal("retry/privacy")
			}
		}
	}
	s := &repositoryStub{run: func(ctx context.Context) { <-ctx.Done() }, err: user.ErrTimeout}
	h := testHandler(s, io.Discard)
	h.databaseTimeout = time.Millisecond
	response(t, h, request("POST", listPath, `{"name":"x","url":"https://example.com/"}`), 503, "database_timeout")
	s = &repositoryStub{}
	h = testHandler(s, io.Discard)
	h.processingTimeout = 20 * time.Millisecond
	reader, writer := io.Pipe()
	defer writer.Close()
	r := request("POST", listPath, "")
	r.Body = reader
	response(t, h, r, 503, "service_unavailable")
	if s.calls.Load() != 0 {
		t.Fatal("slow body work")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request("GET", listPath, "").WithContext(ctx))
	if w.Body.Len() != 0 || s.calls.Load() != 0 {
		t.Fatal("canceled")
	}
	var logs bytes.Buffer
	s = &repositoryStub{run: func(context.Context) { panic("PRIVATE panic SQL") }}
	response(t, testHandler(s, &logs), request("GET", listPath, ""), 500, "internal_error")
	if strings.Contains(logs.String(), "PRIVATE") {
		t.Fatal("panic leak")
	}
}

// This architectural regression guard enforces the project's supported writer
// boundary. Privileged SQL/DDL is not a user authorization mechanism.
func TestMonitorAttributeWritesStayInStore(t *testing.T) {
	for _, root := range []string{"../..//internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "monitor/store.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s := strings.ToLower(string(b))
			for _, sql := range []string{"update http_monitors", "insert into http_monitors"} {
				if strings.Contains(s, sql) {
					t.Errorf("Project attribute writer outside Store: %s", p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
