package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

const exampleID = "019a5abc-1234-7123-8123-123456789abc"

type repositoryStub struct {
	calls   atomic.Int32
	members func(context.Context, session.Digest, string, int, string) (MemberPage, error)
	remove  func(context.Context, session.Digest, string, string) error
	create  func(context.Context, session.Digest, string) (Public, error)
	detail  func(context.Context, session.Digest, string) (Public, error)
	list    func(context.Context, session.Digest, int, string) (Page, error)
}

func (s *repositoryStub) Create(ctx context.Context, d session.Digest, n string) (Public, error) {
	s.calls.Add(1)
	if s.create != nil {
		return s.create(ctx, d, n)
	}
	return Public{exampleID, n, "owner", time.Unix(0, 0).UTC(), time.Unix(0, 0).UTC()}, nil
}
func (s *repositoryStub) Detail(ctx context.Context, d session.Digest, id string) (Public, error) {
	s.calls.Add(1)
	if s.detail != nil {
		return s.detail(ctx, d, id)
	}
	return Public{exampleID, "团队", "member", time.Unix(0, 0).UTC(), time.Unix(0, 0).UTC()}, nil
}
func (s *repositoryStub) List(ctx context.Context, d session.Digest, n int, after string) (Page, error) {
	s.calls.Add(1)
	if s.list != nil {
		return s.list(ctx, d, n, after)
	}
	return Page{Data: []Public{}}, nil
}
func testHandler(store *repositoryStub, logs io.Writer) *Handler {
	origin, _ := config.ParseOrigin("https://app.example.com")
	return New(store, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(logs, nil)))
}
func request(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	r.Header.Set("Cookie", "__Host-devpilot_session="+strings.Repeat("A", 43))
	return r
}
func response(t *testing.T, h *Handler, r *http.Request, status int, code string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || (status != 204 && w.Header().Get("Content-Type") != "application/json") || (status == 204 && (w.Header().Get("Content-Type") != "" || w.Body.Len() != 0)) || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("safe headers")
	}
	if r.Method == "HEAD" {
		if w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
		return w
	}
	if code != "" {
		var b struct {
			Error struct{ Code, Message string }
		}
		if json.Unmarshal(w.Body.Bytes(), &b) != nil || b.Error.Code != code || b.Error.Message != messages[code] {
			t.Fatal("fixed error contract")
		}
	}
	return w
}

func TestStrictInputAndNamePolicy(t *testing.T) {
	for _, tc := range []struct {
		body         string
		status       int
		code, reason string
	}{
		{"", 400, "invalid_request", ""}, {"null", 400, "invalid_request", ""}, {"[]", 400, "invalid_request", ""}, {"{}", 400, "invalid_request", ""},
		{`{"name":null}`, 400, "invalid_request", ""}, {`{"name":2}`, 400, "invalid_request", ""}, {`{"name":"x","name":"y"}`, 400, "invalid_request", ""},
		{`{"name":"x"} {}`, 400, "invalid_request", ""}, {`{"name":"x","user_id":"other"}`, 400, "invalid_request", ""},
		{`{"name":"x","role":"owner"}`, 400, "invalid_request", ""}, {`{"name":"\ud800"}`, 400, "invalid_request", ""},
		{`{"name":"\udc00"}`, 400, "invalid_request", ""}, {`{"name":"\ud800\u1234"}`, 400, "invalid_request", ""},
		{`{"name":"\udc00\ud800"}`, 400, "invalid_request", ""}, {`{"name":"\u12"}`, 400, "invalid_request", ""},
		{"{\"name\":\"\xff\"}", 400, "invalid_request", ""},
		{`{"name":"   \t\r\n"}`, 422, "validation_failed", "required"},
		{`{"name":"a\nb"}`, 422, "validation_failed", "invalid_format"}, {`{"name":"a\u0000b"}`, 422, "validation_failed", "invalid_format"},
		{`{"name":"a\u0085b"}`, 422, "validation_failed", "invalid_format"}, {`{"name":"\u00a0x"}`, 422, "validation_failed", "invalid_format"},
		{`{"name":"x\u3000"}`, 422, "validation_failed", "invalid_format"},
		{`{"name":"` + strings.Repeat("x", 101) + `"}`, 422, "validation_failed", "invalid_length"},
		{`{"name":"` + strings.Repeat("🔐", 101) + `"}`, 422, "validation_failed", "invalid_length"},
		{`{"name":"` + strings.Repeat("x", 4090) + `"}`, 413, "request_too_large", ""},
		{`{"name":"x"}` + strings.Repeat(" ", 4096), 413, "request_too_large", ""},
	} {
		s := &repositoryStub{}
		h := testHandler(s, io.Discard)
		r := request("POST", collection, tc.body)
		r.ContentLength = -1
		w := response(t, h, r, tc.status, tc.code)
		if s.calls.Load() != 0 {
			t.Fatal("invalid input reached database")
		}
		if tc.reason != "" {
			var out struct {
				Error struct{ Fields map[string]string }
			}
			json.Unmarshal(w.Body.Bytes(), &out)
			if out.Error.Fields["name"] != tc.reason || len(out.Error.Fields) != 1 {
				t.Fatal("field reason")
			}
		}
	}
	for _, name := range []string{"x", strings.Repeat("🔐", 100), "研发 团队", "\ufffd", "' OR 1=1; <script>", "e\u0301"} {
		s := &repositoryStub{}
		h := testHandler(s, io.Discard)
		b, _ := json.Marshal(map[string]string{"name": " \t" + name + "\r\n"})
		w := response(t, h, request("POST", collection, string(b)), 201, "")
		var out struct{ Data Public }
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.Data.Name != name || w.Header().Get("Location") != collection+"/"+exampleID {
			t.Fatal("name/projection changed")
		}
	}
	h := testHandler(&repositoryStub{}, io.Discard)
	bounded := `{"name":"x"}`
	bounded += strings.Repeat(" ", 4096-len(bounded))
	r := request("POST", collection, bounded)
	r.Header.Set("Content-Type", "application/json; charset=UTF-8")
	response(t, h, r, 201, "")
	w := response(t, h, request("POST", collection, `{"name":"\ud83d\udd10"}`), 201, "")
	if !strings.Contains(w.Body.String(), "🔐") {
		t.Fatal("valid surrogate pair")
	}
}

func TestRepresentationMethodsAndSelectors(t *testing.T) {
	for _, media := range []string{"", "text/plain", "application/json; charset=latin1", "application/json; unexpected=1"} {
		s := &repositoryStub{}
		r := request("POST", collection, `{"name":"x"}`)
		r.Header.Set("Content-Type", media)
		response(t, testHandler(s, io.Discard), r, 415, "unsupported_media_type")
		if s.calls.Load() != 0 {
			t.Fatal("unsupported media DB")
		}
	}
	for _, header := range []string{"Content-Type", "Content-Encoding"} {
		s := &repositoryStub{}
		r := request("POST", collection, `{"name":"x"}`)
		if header == "Content-Type" {
			r.Header.Add(header, "application/json")
		} else {
			r.Header[header] = []string{""}
		}
		response(t, testHandler(s, io.Discard), r, 415, "unsupported_media_type")
	}
	for _, path := range []string{collection, collection + "/" + exampleID} {
		for _, method := range []string{"PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
			s := &repositoryStub{}
			w := response(t, testHandler(s, io.Discard), request(method, path, ""), 405, "method_not_allowed")
			allow := "GET"
			if path == collection {
				allow = "GET, POST"
			}
			if w.Header().Get("Allow") != allow || s.calls.Load() != 0 {
				t.Fatal("method contract")
			}
		}
	}
	for _, q := range []string{"user_id=other", "limit=0", "limit=101", "limit=01", "limit=+1", "limit=-1", "limit=1&limit=2", "after=bad", "after=", "after=" + exampleID + "&after=" + exampleID, "%xx=x", "x=%zz", "limit=1;after=bad"} {
		s := &repositoryStub{}
		response(t, testHandler(s, io.Discard), request("GET", collection+"?"+q, ""), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("invalid selectors DB")
		}
	}
	for _, method := range []string{"POST", "GET"} {
		path := collection
		if method == "GET" {
			path += "/" + exampleID
		}
		response(t, testHandler(&repositoryStub{}, io.Discard), request(method, path+"?limit=1", `{"name":"x"}`), 400, "invalid_request")
	}
	s := &repositoryStub{list: func(_ context.Context, _ session.Digest, n int, after string) (Page, error) {
		if n != 100 || after != exampleID {
			t.Fatal("selectors")
		}
		return Page{Data: []Public{}}, nil
	}}
	response(t, testHandler(s, io.Discard), request("GET", collection+"?limit=100&after="+exampleID, "ignored"), 200, "")
}

func TestCSRFAndCookieReuse(t *testing.T) {
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Set("Origin", "null") },
		func(r *http.Request) { r.Header.Add("Origin", "https://app.example.com") }, func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com.attacker.test") },
		func(r *http.Request) { r.Header.Set("Origin", "https://sub.app.example.com") }, func(r *http.Request) { r.Header.Set("Origin", "http://app.example.com") },
		func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com:444") }, func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com/path") },
		func(r *http.Request) { r.Header.Del("Origin"); r.Header.Set("Referer", "https://app.example.com") },
		func(r *http.Request) { r.Header.Del("X-DevPilot-CSRF") }, func(r *http.Request) { r.Header.Add("X-DevPilot-CSRF", "1") },
		func(r *http.Request) { r.Header.Set("X-DevPilot-CSRF", "2") }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") },
		func(r *http.Request) { r.Header["Sec-Fetch-Site"] = []string{"same-origin", "same-origin"} },
		func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.test")
			r.Host = "app.example.com"
			r.Header.Set("Forwarded", "host=app.example.com;proto=https")
			r.Header.Set("X-Forwarded-Host", "app.example.com")
		},
	} {
		s := &repositoryStub{}
		r := request("POST", collection, `{"name":"x"}`)
		mutate(r)
		response(t, testHandler(s, io.Discard), r, 403, "csrf_rejected")
		if s.calls.Load() != 0 {
			t.Fatal("CSRF reached DB")
		}
	}
	for _, cookie := range []string{"", "__Host-devpilot_session=bad", "__Host-devpilot_session=" + strings.Repeat("A", 43) + "; __Host-devpilot_session=" + strings.Repeat("A", 43), "__Host-devpilot_session= " + strings.Repeat("A", 43), "__Host-devpilot_session=" + strings.Repeat("A", 43) + " ", "devpilot_session=" + strings.Repeat("A", 43)} {
		s := &repositoryStub{}
		r := request("GET", collection, "")
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("A", 43))
		response(t, testHandler(s, io.Discard), r, 401, "unauthenticated")
		if s.calls.Load() != 0 {
			t.Fatal("unusable Cookie reached DB")
		}
	}
	// Read-only requests require neither proof header, and development selects
	// exactly the existing environment's cookie name.
	r := request("GET", collection, "")
	r.Header.Del("Origin")
	r.Header.Del("X-DevPilot-CSRF")
	h := testHandler(&repositoryStub{}, io.Discard)
	response(t, h, r, 200, "")
	h.policy.Mode = "development"
	r.Header.Set("Cookie", "devpilot_session="+strings.Repeat("A", 43))
	response(t, h, r, 200, "")
}

func TestFailuresPrivacyCancellationAndBudgets(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{session.ErrInvalid, 401, "unauthenticated"}, {ErrNotFound, 404, "workspace_not_found"},
		{user.ErrUnavailable, 503, "service_unavailable"}, {user.ErrTimeout, 503, "database_timeout"},
		{user.ErrSchema, 500, "internal_error"}, {user.ErrConflict, 500, "internal_error"}, {errors.New("SENSITIVE token digest PHC DSN SQL name email password"), 500, "internal_error"},
	} {
		for _, method := range []string{"GET", "POST"} {
			var logs bytes.Buffer
			s := &repositoryStub{create: func(context.Context, session.Digest, string) (Public, error) { return Public{}, tc.err }, list: func(context.Context, session.Digest, int, string) (Page, error) { return Page{}, tc.err }}
			w := response(t, testHandler(s, &logs), request(method, collection, `{"name":"SENSITIVE"}`), tc.status, tc.code)
			if strings.Contains(logs.String()+w.Body.String(), "SENSITIVE") || w.Header().Get("Location") != "" {
				t.Fatal("sensitive failure/unknown commit success")
			}
			if s.calls.Load() != 1 {
				t.Fatal("automatic retry")
			}
		}
	}
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	r := request("GET", collection, "")
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Body.Len() != 0 || s.calls.Load() != 0 {
		t.Fatal("already canceled request did work")
	}
	s.list = func(ctx context.Context, _ session.Digest, _ int, _ string) (Page, error) {
		cancel()
		return Page{}, user.ErrCanceled
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request("GET", collection, "").WithContext(ctx))
	if w.Body.Len() != 0 {
		t.Fatal("attempted canceled response")
	}
	s.list = func(ctx context.Context, _ session.Digest, _ int, _ string) (Page, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("DB budget")
		}
		<-ctx.Done()
		return Page{}, user.ErrTimeout
	}
	h.databaseTimeout = time.Millisecond
	response(t, h, request("GET", collection, ""), 503, "database_timeout")
	h.processingTimeout = time.Nanosecond
	response(t, h, request("GET", collection, ""), 503, "service_unavailable")
}

type panicWriter struct{ http.ResponseWriter }

func (w panicWriter) Write(b []byte) (int, error) { panic("SENSITIVE post-write panic") }

func TestPanicAndConcurrentRequestIsolation(t *testing.T) {
	var logs bytes.Buffer
	s := &repositoryStub{list: func(context.Context, session.Digest, int, string) (Page, error) { panic("SENSITIVE pre-write panic") }}
	h := testHandler(s, &logs)
	response(t, h, request("GET", collection, ""), 500, "internal_error")
	if strings.Contains(logs.String(), "SENSITIVE") {
		t.Fatal("panic leaked")
	}
	s.list = nil
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Fatal("post-write panic must abort")
			}
		}()
		h.ServeHTTP(panicWriter{httptest.NewRecorder()}, request("GET", collection, ""))
	}()
	if strings.Contains(logs.String(), "SENSITIVE") {
		t.Fatal("post-write panic leaked")
	}
	h = testHandler(&repositoryStub{}, io.Discard)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := `{"name":"x"}`
			status := 201
			code := ""
			if i%2 == 0 {
				body = `{"name":"x","role":"owner"}`
				status = 400
				code = "invalid_request"
			}
			response(t, h, request("POST", collection, body), status, code)
		}(i)
	}
	wg.Wait()
}

func FuzzWorkspaceNameJSON(f *testing.F) {
	for _, s := range []string{`{"name":"x"}`, `{"name":"\ud800"}`, `{"name":"\ud83d\udd10"}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 4096 {
			return
		}
		name, ok := parseName(b)
		if ok {
			nameReason(name)
		}
	})
}
