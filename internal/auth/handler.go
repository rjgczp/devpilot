// Package auth coordinates the three bounded Cookie-session endpoints.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

type Users interface {
	FindCredentials(context.Context, string) (user.Credentials, error)
}
type Sessions interface {
	Create(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error)
	Lookup(context.Context, session.Digest) (user.Public, error)
	Revoke(context.Context, session.Digest) error
}
type Verifier interface {
	Verify(string, string) (bool, error)
}
type Tokens interface {
	New() (string, session.Digest, error)
}

type Handler struct {
	users                              Users
	sessions                           Sessions
	verifier                           Verifier
	tokens                             Tokens
	budget                             *password.Budget
	policy                             config.AuthConfig
	logger                             *slog.Logger
	workflows                          chan struct{}
	processingTimeout, databaseTimeout time.Duration
}

func New(users Users, sessions Sessions, verifier Verifier, tokens Tokens, budget *password.Budget, policy config.AuthConfig, logger *slog.Logger) *Handler {
	return &Handler{users: users, sessions: sessions, verifier: verifier, tokens: tokens, budget: budget, policy: policy, logger: logger, workflows: make(chan struct{}, 2), processingTimeout: 5 * time.Second, databaseTimeout: 2 * time.Second}
}

type writer struct {
	http.ResponseWriter
	written bool
}

func (w *writer) WriteHeader(status int)      { w.written = true; w.ResponseWriter.WriteHeader(status) }
func (w *writer) Write(b []byte) (int, error) { w.written = true; return w.ResponseWriter.Write(b) }

func (h *Handler) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	w := &writer{ResponseWriter: out}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(http.ErrAbortHandler)
			}
			h.logger.Error("authentication failed", "reason", "internal_panic")
			if w.written {
				panic(http.ErrAbortHandler)
			}
			h.fail(w, r, 500, "internal_error", nil)
		}
	}()
	if r.Context().Err() != nil {
		return
	}
	method := http.MethodPost
	if r.URL.Path == "/api/v1/auth/me" {
		method = http.MethodGet
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	if method == http.MethodPost && !h.csrf(r) {
		h.fail(w, r, 403, "csrf_rejected", nil)
		return
	}
	switch r.URL.Path {
	case "/api/v1/auth/login":
		h.login(w, r)
	case "/api/v1/auth/logout":
		h.logout(w, r)
	case "/api/v1/auth/me":
		h.me(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) csrf(r *http.Request) bool { return ValidCSRF(r, h.policy) }

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.processingTimeout)
	defer cancel()
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	valid := err == nil && media == "application/json" && len(r.Header.Values("Content-Type")) == 1
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			valid = false
		}
	}
	_, encoded := r.Header["Content-Encoding"]
	if !valid || encoded {
		h.fail(w, r, 415, "unsupported_media_type", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, credentials.MaxBodyBytes+1))
	if r.Context().Err() != nil {
		return
	}
	if len(body) > credentials.MaxBodyBytes {
		h.fail(w, r, 413, "request_too_large", nil)
		return
	}
	if err != nil {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	in, err := credentials.Parse(body)
	if err != nil {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	if fields := credentials.Validate(&in, 1); len(fields) != 0 {
		h.fail(w, r, 422, "validation_failed", fields)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	select {
	case h.workflows <- struct{}{}:
	default:
		h.busy(w, r)
		return
	}
	defer func() { <-h.workflows }()
	dbctx, cancelDB := context.WithTimeout(ctx, h.databaseTimeout)
	snapshot, err := h.users.FindCredentials(dbctx, in.Email)
	cancelDB()
	if h.canceled(r, err) {
		return
	}
	found := err == nil
	if err != nil && !errors.Is(err, user.ErrNotFound) {
		h.databaseError(w, r, err)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	validPHC := found && password.Valid(snapshot.PasswordHash)
	phc := password.DummyPHC
	if validPHC {
		phc = snapshot.PasswordHash
	} else if found {
		h.logger.Error("authentication failed", "reason", "invalid_password_hash")
	}
	match, busy, err := h.verify(in.Password, phc)
	if r.Context().Err() != nil {
		return
	}
	if busy {
		h.busy(w, r)
		return
	}
	if err != nil {
		h.logger.Error("authentication failed", "reason", "password_verify_error")
		h.fail(w, r, 500, "internal_error", nil)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	if !found || !validPHC || !match || snapshot.Status != "active" {
		h.fail(w, r, 401, "invalid_credentials", nil)
		return
	}
	token, digest, err := h.tokens.New()
	if err != nil {
		h.logger.Error("authentication failed", "reason", "session_random_error")
		h.fail(w, r, 500, "internal_error", nil)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	dbctx, cancelDB = context.WithTimeout(ctx, h.databaseTimeout)
	created, err := h.sessions.Create(dbctx, snapshot, digest, h.policy.SessionTTL)
	cancelDB()
	if h.canceled(r, err) {
		return
	}
	if errors.Is(err, session.ErrInvalid) {
		h.fail(w, r, 401, "invalid_credentials", nil)
		return
	}
	if err != nil {
		h.databaseError(w, r, err)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	remaining := time.Until(created.ExpiresAt)
	if remaining > h.policy.SessionTTL {
		remaining = h.policy.SessionTTL
	}
	seconds := int(remaining / time.Second)
	if seconds < 1 {
		h.logger.Error("authentication failed", "reason", "invalid_cookie_lifetime")
		h.fail(w, r, 500, "internal_error", nil)
		return
	}
	cookie := h.cookie(token)
	cookie.MaxAge = seconds
	cookie.Expires = created.ExpiresAt.UTC()
	http.SetCookie(w, cookie)
	h.write(w, r, 200, map[string]any{"data": created.User})
}

func (h *Handler) verify(secret, phc string) (bool, bool, error) {
	if !h.budget.Acquire() {
		return false, true, nil
	}
	defer h.budget.Release()
	match, err := h.verifier.Verify(secret, phc)
	return match, false, err
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	digest, valid := session.CookieDigest(r, h.cookieName())
	if !valid {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.databaseTimeout)
	defer cancel()
	u, err := h.sessions.Lookup(ctx, digest)
	if h.canceled(r, err) {
		return
	}
	if errors.Is(err, session.ErrInvalid) {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	if err != nil {
		h.databaseError(w, r, err)
		return
	}
	if ctx.Err() != nil {
		h.databaseError(w, r, user.ErrTimeout)
		return
	}
	h.write(w, r, 200, map[string]any{"data": u})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	digest, valid := session.CookieDigest(r, h.cookieName())
	if valid {
		ctx, cancel := context.WithTimeout(r.Context(), h.databaseTimeout)
		defer cancel()
		err := h.sessions.Revoke(ctx, digest)
		if h.canceled(r, err) {
			return
		}
		if err != nil {
			h.databaseError(w, r, err)
			return
		}
		if ctx.Err() != nil {
			h.databaseError(w, r, user.ErrTimeout)
			return
		}
	}
	if r.Context().Err() != nil {
		return
	}
	cookie := h.cookie("")
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0).UTC()
	http.SetCookie(w, cookie)
	h.write(w, r, 204, nil)
}

func (h *Handler) cookieName() string { return CookieName(h.policy) }
func (h *Handler) cookie(value string) *http.Cookie {
	return &http.Cookie{Name: h.cookieName(), Value: value, Path: "/", Secure: h.policy.Mode != "development", HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

func (h *Handler) canceled(r *http.Request, err error) bool {
	return r.Context().Err() != nil || errors.Is(err, user.ErrCanceled)
}
func (h *Handler) expired(w http.ResponseWriter, r *http.Request, ctx context.Context) bool {
	if r.Context().Err() != nil {
		return true
	}
	// Context timer delivery can lag the actual deadline under scheduler load.
	deadline, hasDeadline := ctx.Deadline()
	if ctx.Err() != nil || (hasDeadline && !time.Now().Before(deadline)) {
		h.fail(w, r, 503, "service_unavailable", nil)
		return true
	}
	return false
}
func (h *Handler) busy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "1")
	h.fail(w, r, 503, "authentication_busy", nil)
}
func (h *Handler) databaseError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, reason := 500, "internal_error", "internal_error"
	switch {
	case errors.Is(err, user.ErrUnavailable):
		status, code, reason = 503, "service_unavailable", "db_unavailable"
	case errors.Is(err, user.ErrTimeout):
		status, code, reason = 503, "database_timeout", "db_timeout"
	case errors.Is(err, user.ErrSchema):
		reason = "schema_missing"
	}
	h.logger.Error("authentication failed", "reason", reason, "status", status)
	h.fail(w, r, status, code, nil)
}

var messages = map[string]string{
	"invalid_request": "Invalid login request.", "csrf_rejected": "Request origin could not be verified.",
	"method_not_allowed": "Method not allowed.", "request_too_large": "Request body too large.",
	"unsupported_media_type": "Unsupported media type.", "validation_failed": "Invalid login fields.",
	"invalid_credentials": "Invalid email or password.", "unauthenticated": "Authentication required.",
	"authentication_busy": "Authentication is temporarily busy.",
	"service_unavailable": "Authentication operation could not be confirmed.",
	"database_timeout":    "Authentication operation could not be confirmed.",
	"internal_error":      "Unable to complete authentication operation.",
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, code string, fields map[string]string) {
	e := map[string]any{"code": code, "message": messages[code]}
	if len(fields) > 0 {
		e["fields"] = fields
	}
	h.write(w, r, status, map[string]any{"error": e})
}
func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, body any) {
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if status != 204 {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if status != 204 && r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(body)
	}
}
