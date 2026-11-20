package ai

import (
	"context"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const id = "019a5abc-1234-7123-8123-123456789abc"
const requestBody = `{"workspace_id":"` + id + `","project_id":"` + id + `","monitor_id":"` + id + `"}`

type scopedStore struct {
	detailCalls, historyCalls int
	err                       error
	workspace                 string
}

func (s *scopedStore) Detail(_ context.Context, _ session.Digest, w, p, m string) (monitor.Public, error) {
	s.detailCalls++
	s.workspace = w
	if w != id {
		return monitor.Public{}, monitor.ErrNotFound
	}
	return monitor.Public{}, s.err
}
func (s *scopedStore) History(_ context.Context, _ session.Digest, w, p, m string, q monitor.HistoryQuery) (monitor.HistoryPage, error) {
	s.historyCalls++
	if w != s.workspace || q.Limit != 10 || q.State != "all" {
		panic("wrong scope/bound")
	}
	return monitor.HistoryPage{}, nil
}

type diagnoseFunc func(context.Context, monitor.Public, monitor.HistoryPage) (Result, error)

func (f diagnoseFunc) Diagnose(c context.Context, m monitor.Public, h monitor.HistoryPage) (Result, error) {
	return f(c, m, h)
}
func newTestHandler(s *scopedStore) *Handler {
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := New(s, config.AuthConfig{Mode: "production", Origin: origin}, config.AIConfig{Timeout: 50 * time.Millisecond})
	h.model = diagnoseFunc(func(context.Context, monitor.Public, monitor.HistoryPage) (Result, error) {
		return Result{Summary: "ok"}, nil
	})
	return h
}
func req(body string) *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/ai/diagnose", strings.NewReader(body))
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cookie", "__Host-devpilot_session="+strings.Repeat("A", 43))
	return r
}
func status(t *testing.T, h *Handler, r *http.Request, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("got%d want%d %s", w.Code, want, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache")
	}
	return w
}
func TestAuthorizationBeforeModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		mutate func(*http.Request)
		err    error
		want   int
		db     int
	}{
		{name: "csrf", body: requestBody, mutate: func(r *http.Request) { r.Header.Del("Origin") }, want: 403},
		{name: "cookie", body: requestBody, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, want: 401},
		{name: "expired", body: requestBody, err: session.ErrInvalid, want: 401, db: 1},
		{name: "cross_workspace", body: strings.Replace(requestBody, id, "019a5abc-1234-7123-8123-123456789abd", 1), want: 404, db: 1},
		{name: "outsider", body: requestBody, err: monitor.ErrNotFound, want: 404, db: 1},
		{name: "bad_id", body: `{"workspace_id":"bad"}`, want: 400},
		{name: "unknown_input", body: strings.Replace(requestBody, "}", `,"url":"http://evil"}`, 1), want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &scopedStore{err: tc.err}
			h := newTestHandler(s)
			calls := 0
			h.model = diagnoseFunc(func(context.Context, monitor.Public, monitor.HistoryPage) (Result, error) {
				calls++
				return Result{}, nil
			})
			r := req(tc.body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			status(t, h, r, tc.want)
			if calls != 0 || s.historyCalls != 0 || s.detailCalls != tc.db {
				t.Fatal("unauthorized dependency called")
			}
		})
	}
}
func TestHandlerTimeoutFailureAndAdmission(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		h := newTestHandler(&scopedStore{})
		h.model = diagnoseFunc(func(c context.Context, _ monitor.Public, _ monitor.HistoryPage) (Result, error) {
			<-c.Done()
			return Result{}, c.Err()
		})
		status(t, h, req(requestBody), 504)
	})
	t.Run("failure", func(t *testing.T) {
		h := newTestHandler(&scopedStore{})
		h.model = diagnoseFunc(func(context.Context, monitor.Public, monitor.HistoryPage) (Result, error) {
			return Result{}, ErrProvider
		})
		w := status(t, h, req(requestBody), 502)
		if strings.Contains(w.Body.String(), "server-secret") {
			t.Fatal("secret")
		}
	})
	t.Run("disabled", func(t *testing.T) {
		h := newTestHandler(&scopedStore{})
		h.model = nil
		status(t, h, req(requestBody), 503)
	})
	t.Run("cooldown", func(t *testing.T) {
		h := newTestHandler(&scopedStore{})
		status(t, h, req(requestBody), 200)
		w := status(t, h, req(requestBody), 429)
		if w.Header().Get("Retry-After") != "30" {
			t.Fatal("retry")
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		h := newTestHandler(&scopedStore{})
		h.active <- struct{}{}
		h.active <- struct{}{}
		status(t, h, req(requestBody), 429)
	})
	t.Run("oversized", func(t *testing.T) {
		h := newTestHandler(&scopedStore{})
		status(t, h, req(requestBody+strings.Repeat(" ", 1024)), 413)
	})
}
