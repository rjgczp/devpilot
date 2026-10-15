package workspace

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

	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

const collection = "/api/v1/workspaces"

type Repository interface {
	Create(context.Context, session.Digest, string) (Public, error)
	Detail(context.Context, session.Digest, string) (Public, error)
	List(context.Context, session.Digest, int, string) (Page, error)
	ListMembers(context.Context, session.Digest, string, int, string) (MemberPage, error)
	DeleteMember(context.Context, session.Digest, string, string) error
}

type Handler struct {
	store                              Repository
	policy                             config.AuthConfig
	logger                             *slog.Logger
	processingTimeout, databaseTimeout time.Duration
}

func New(store Repository, policy config.AuthConfig, logger *slog.Logger) *Handler {
	return &Handler{store: store, policy: policy, logger: logger, processingTimeout: 5 * time.Second, databaseTimeout: 2 * time.Second}
}

type responseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) WriteHeader(status int) {
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}

func (h *Handler) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	w := &responseWriter{ResponseWriter: out}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(http.ErrAbortHandler)
			}
			h.logger.Error("workspace operation failed", "reason", "internal_panic")
			if w.written {
				panic(http.ErrAbortHandler)
			}
			h.fail(w, r, 500, "internal_error", nil)
		}
	}()
	if r.Context().Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.processingTimeout)
	defer cancel()
	if strings.HasPrefix(r.URL.Path, collection+"/") && strings.Contains(strings.TrimPrefix(r.URL.Path, collection+"/"), "/members") {
		h.serveMembers(w, r, ctx)
		return
	}
	isCollection := r.URL.Path == collection
	id := strings.TrimPrefix(r.URL.Path, collection+"/")
	if !isCollection && (id == r.URL.Path || id == "" || strings.Contains(id, "/")) {
		http.NotFound(w, r)
		return
	}
	allow := "GET"
	if isCollection {
		allow = "GET, POST"
	}
	if r.Method != http.MethodGet && !(isCollection && r.Method == http.MethodPost) {
		w.Header().Set("Allow", allow)
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	if r.Method == http.MethodPost && !auth.ValidCSRF(r, h.policy) {
		h.fail(w, r, 403, "csrf_rejected", nil)
		return
	}
	digest, valid := session.CookieDigest(r, auth.CookieName(h.policy))
	if !valid {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	limit, after, valid := selectors(r.URL.RawQuery, isCollection && r.Method == http.MethodGet)
	if !valid {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	var name string
	if r.Method == http.MethodPost {
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
		name, valid = parseName(body)
		if !valid {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
		if reason := nameReason(name); reason != "" {
			h.fail(w, r, 422, "validation_failed", map[string]string{"name": reason})
			return
		}
	}
	if r.Context().Err() != nil {
		return
	}
	if ctx.Err() != nil {
		h.fail(w, r, 503, "service_unavailable", nil)
		return
	}
	dbctx, cancelDB := context.WithTimeout(ctx, h.databaseTimeout)
	defer cancelDB()
	var result any
	var err error
	status, location := 200, ""
	switch {
	case r.Method == http.MethodPost:
		var created Public
		created, err = h.store.Create(dbctx, digest, name)
		result = map[string]any{"data": created}
		status, location = 201, collection+"/"+created.ID
	case isCollection:
		result, err = h.store.List(dbctx, digest, limit, after)
	default:
		var detail Public
		detail, err = h.store.Detail(dbctx, digest, id)
		result = map[string]any{"data": detail}
	}
	if r.Context().Err() != nil || errors.Is(err, user.ErrCanceled) {
		return
	}
	if err == nil && dbctx.Err() != nil {
		err = user.ErrTimeout
	}
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	if location != "" {
		w.Header().Set("Location", location)
	}
	h.write(w, r, status, result)
}

func (h *Handler) operationError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, reason := 500, "internal_error", "internal_error"
	switch {
	case errors.Is(err, session.ErrInvalid):
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	case errors.Is(err, ErrNotFound):
		h.fail(w, r, 404, "workspace_not_found", nil)
		return
	case errors.Is(err, ErrMemberForbidden):
		h.fail(w, r, 403, "membership_forbidden", nil)
		return
	case errors.Is(err, ErrMemberNotFound):
		h.fail(w, r, 404, "member_not_found", nil)
		return
	case errors.Is(err, ErrLastOwner):
		h.fail(w, r, 409, "last_owner_required", nil)
		return
	case errors.Is(err, user.ErrUnavailable):
		status, code, reason = 503, "service_unavailable", "db_unavailable"
	case errors.Is(err, user.ErrTimeout):
		status, code, reason = 503, "database_timeout", "db_timeout"
	case errors.Is(err, user.ErrSchema):
		reason = "schema_missing"
	}
	h.logger.Error("workspace operation failed", "reason", reason, "status", status)
	h.fail(w, r, status, code, nil)
}

var messages = map[string]string{
	"membership_forbidden": "Membership operation is not permitted.",
	"member_not_found":     "Member not found.",
	"last_owner_required":  "Workspace must retain an owner.",
	"invalid_request":      "Invalid workspace request.", "unauthenticated": "Authentication required.",
	"csrf_rejected": "Request origin could not be verified.", "workspace_not_found": "Workspace not found.",
	"method_not_allowed": "Method not allowed.", "request_too_large": "Request body too large.",
	"unsupported_media_type": "Unsupported media type.", "validation_failed": "Invalid workspace fields.",
	"service_unavailable": "Workspace operation could not be confirmed.",
	"database_timeout":    "Workspace operation could not be confirmed.",
	"internal_error":      "Unable to complete workspace operation.",
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, code string, fields map[string]string) {
	e := map[string]any{"code": code, "message": messages[code]}
	if len(fields) != 0 {
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
	if status != http.StatusNoContent {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead && status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(body)
	}
}
