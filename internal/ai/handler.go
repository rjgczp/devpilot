package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
)

type Repository interface {
	Detail(context.Context, session.Digest, string, string, string) (monitor.Public, error)
	History(context.Context, session.Digest, string, string, string, monitor.HistoryQuery) (monitor.HistoryPage, error)
}
type Diagnoser interface {
	Diagnose(context.Context, monitor.Public, monitor.HistoryPage) (Result, error)
}
type Handler struct {
	store   Repository
	policy  config.AuthConfig
	model   Diagnoser
	timeout time.Duration
	mu      sync.Mutex
	active  chan struct{}
	recent  map[session.Digest]time.Time
}

func New(s Repository, p config.AuthConfig, c config.AIConfig) *Handler {
	h := &Handler{store: s, policy: p, timeout: c.Timeout, active: make(chan struct{}, 2), recent: make(map[session.Digest]time.Time)}
	if h.timeout <= 0 {
		h.timeout = 20 * time.Second
	}
	if c.BaseURL != "" && c.Model != "" && c.APIKey != "" {
		h.model = NewClient(c)
	}
	return h
}

type input struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	MonitorID   string `json:"monitor_id"`
}

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/api/v1/ai/diagnose" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(w, 404, "not_found")
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "method_not_allowed")
		return
	}
	if !auth.ValidCSRF(r, h.policy) {
		fail(w, 403, "csrf_rejected")
		return
	}
	digest, ok := session.CookieDigest(r, auth.CookieName(h.policy))
	if !ok {
		fail(w, 401, "unauthenticated")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	c := http.NewResponseController(w)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = c.SetWriteDeadline(time.Now().Add(h.timeout + time.Second))
	media, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" {
		fail(w, 415, "unsupported_media_type")
		return
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			fail(w, 415, "unsupported_media_type")
			return
		}
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 1025))
	if len(body) > 1024 {
		w.Header().Set("Connection", "close")
		fail(w, 413, "request_too_large")
		return
	}
	var in input
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	if e != nil || d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF || !uuid.MatchString(in.WorkspaceID) || !uuid.MatchString(in.ProjectID) || !uuid.MatchString(in.MonitorID) {
		fail(w, 400, "invalid_request")
		return
	}
	// Reuse the existing member-scoped read, including live session expiry and
	// workspace/project/monitor tuple checks. Never call the model before this.
	m, e := h.store.Detail(ctx, digest, in.WorkspaceID, in.ProjectID, in.MonitorID)
	if e != nil {
		storeError(w, e)
		return
	}
	if h.model == nil {
		fail(w, 503, "ai_unavailable")
		return
	}
	if !h.admit(digest) {
		w.Header().Set("Retry-After", "30")
		fail(w, 429, "ai_busy")
		return
	}
	defer func() { <-h.active }()
	history, e := h.store.History(ctx, digest, in.WorkspaceID, in.ProjectID, in.MonitorID, monitor.HistoryQuery{Limit: 10, State: "all"})
	if e != nil {
		storeError(w, e)
		return
	}
	result, e := h.model.Diagnose(ctx, m, history)
	if e != nil || ctx.Err() != nil {
		if ctx.Err() != nil || errors.Is(e, context.DeadlineExceeded) {
			fail(w, 504, "ai_timeout")
		} else {
			fail(w, 502, "ai_provider_failed")
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
}
func (h *Handler) admit(d session.Digest) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for key, t := range h.recent {
		if now.Sub(t) >= 30*time.Second {
			delete(h.recent, key)
		}
	}
	if _, exists := h.recent[d]; exists || len(h.recent) >= 512 {
		return false
	}
	select {
	case h.active <- struct{}{}:
		h.recent[d] = now
		return true
	default:
		return false
	}
}
func storeError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, session.ErrInvalid):
		fail(w, 401, "unauthenticated")
	case errors.Is(e, monitor.ErrNotFound):
		fail(w, 404, "monitor_not_found")
	case errors.Is(e, monitor.ErrForbidden):
		fail(w, 403, "monitor_forbidden")
	default:
		fail(w, 503, "service_unavailable")
	}
}
func fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": "AI diagnosis could not be completed (" + code + ")."}})
}
