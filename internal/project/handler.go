package project

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

const collection = "/api/v1/workspaces/"

type Repository interface {
	Create(context.Context, session.Digest, string, Input) (Public, error)
	Update(context.Context, session.Digest, string, string, Input, string) (Public, error)
	Detail(context.Context, session.Digest, string, string) (Public, error)
	List(context.Context, session.Digest, string, int, string) (Page, error)
}
type Handler struct {
	store                              Repository
	policy                             config.AuthConfig
	logger                             *slog.Logger
	processingTimeout, databaseTimeout time.Duration
}

func New(store Repository, policy config.AuthConfig, logger *slog.Logger) *Handler {
	return &Handler{store, policy, logger, 5 * time.Second, 2 * time.Second}
}

type responseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(n int)           { w.written = true; w.ResponseWriter.WriteHeader(n) }
func (w *responseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}

func (h *Handler) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	w := &responseWriter{ResponseWriter: out}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			h.logger.Error("project operation failed", "reason", "internal_panic")
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
	deadline, _ := ctx.Deadline()
	_ = http.NewResponseController(w).SetReadDeadline(deadline)
	// Keep the transport's implicit body drain bounded on ALL early exits too.
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, collection), "/")
	if !strings.HasPrefix(r.URL.Path, collection) || r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path || (len(parts) != 2 && len(parts) != 3) || parts[0] == "" || parts[1] != "projects" || len(parts) == 3 && parts[2] == "" {
		h.fail(w, r, 404, "project_not_found", nil)
		return
	}
	wid, pid := parts[0], ""
	isCollection := len(parts) == 2
	allow := "GET, POST"
	if !isCollection {
		pid = parts[2]
		allow = "GET, PATCH"
	}
	if r.Method != "GET" && !(isCollection && r.Method == "POST") && !(!isCollection && r.Method == "PATCH") {
		w.Header().Set("Allow", allow)
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	write := r.Method != "GET"
	if write && !auth.ValidCSRF(r, h.policy) {
		h.fail(w, r, 403, "csrf_rejected", nil)
		return
	}
	digest, ok := session.CookieDigest(r, auth.CookieName(h.policy))
	if !ok {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	limit, after, ok := selectors(r.URL.RawQuery, isCollection && !write)
	if !ok {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	for _, key := range []string{"If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range"} {
		if _, exists := r.Header[key]; exists {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
	}
	tag := r.Header.Get("If-Match")
	if values, exists := r.Header["If-Match"]; exists {
		_, _, valid := condition(tag)
		if r.Method != "PATCH" || len(values) != 1 || !valid {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
	}
	var input Input
	if write {
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		ok = err == nil && media == "application/json" && len(r.Header.Values("Content-Type")) == 1 && strings.Count(r.Header.Get("Content-Type"), ";") <= 1
		for k, v := range params {
			if k != "charset" || !strings.EqualFold(v, "utf-8") {
				ok = false
			}
		}
		if _, exists := r.Header["Content-Encoding"]; exists {
			ok = false
		}
		if !ok {
			h.fail(w, r, 415, "unsupported_media_type", nil)
			return
		}
		b, err := readBody(ctx, w, r)
		if r.Context().Err() != nil {
			panic(http.ErrAbortHandler)
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			h.fail(w, r, 503, "service_unavailable", nil)
			return
		}
		if len(b) > MaxBodyBytes {
			h.fail(w, r, 413, "request_too_large", nil)
			return
		}
		input, ok = parseInput(b, isCollection)
		if err != nil || !ok {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
		if fields := validate(input); len(fields) > 0 {
			h.fail(w, r, 422, "validation_failed", fields)
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
	dbctx, stop := context.WithTimeout(ctx, h.databaseTimeout)
	defer stop()
	var result any
	var p Public
	var err error
	status := 200
	switch {
	case r.Method == "POST":
		p, err = h.store.Create(dbctx, digest, wid, input)
		status = 201
	case r.Method == "PATCH":
		p, err = h.store.Update(dbctx, digest, wid, pid, input, tag)
	case isCollection:
		result, err = h.store.List(dbctx, digest, wid, limit, after)
	default:
		p, err = h.store.Detail(dbctx, digest, wid, pid)
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
	if result == nil {
		result = struct {
			Data Public `json:"data"`
		}{p}
		w.Header().Set("ETag", ETag(p))
	}
	if status == 201 {
		w.Header().Set("Location", collection+p.WorkspaceID+"/projects/"+p.ID)
	}
	h.write(w, r, status, result)
}
func readBody(ctx context.Context, w http.ResponseWriter, r *http.Request) ([]byte, error) {
	c := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	supported := c.SetReadDeadline(deadline) == nil
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		if supported {
			_ = c.SetReadDeadline(time.Now())
		} else {
			_ = r.Body.Close()
		}
		close(done)
	})
	b, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if !stop() {
		<-done
	}
	if len(b) > MaxBodyBytes || err != nil || ctx.Err() != nil {
		w.Header().Set("Connection", "close")
		if supported {
			_ = c.SetReadDeadline(time.Now())
		}
	} else if supported {
		_ = c.SetReadDeadline(deadline)
	}
	return b, err
}

var messages = map[string]string{
	"invalid_request": "Invalid project request.", "unauthenticated": "Authentication required.", "csrf_rejected": "Request origin could not be verified.",
	"project_forbidden": "Project operation is not permitted.", "project_not_found": "Project not found.", "method_not_allowed": "Method not allowed.",
	"precondition_failed": "Project precondition failed.", "precondition_required": "Project precondition required.", "request_too_large": "Request body too large.",
	"unsupported_media_type": "Unsupported media type.", "validation_failed": "Invalid project fields.", "database_timeout": "Project operation could not be confirmed.",
	"service_unavailable": "Project operation could not be confirmed.", "internal_error": "Unable to complete project operation.",
}

func (h *Handler) operationError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, reason := 500, "internal_error", "internal_error"
	switch {
	case errors.Is(err, session.ErrInvalid):
		status, code = 401, "unauthenticated"
	case errors.Is(err, ErrNotFound):
		status, code = 404, "project_not_found"
	case errors.Is(err, ErrForbidden):
		status, code = 403, "project_forbidden"
	case errors.Is(err, ErrPreconditionRequired):
		status, code = 428, "precondition_required"
	case errors.Is(err, ErrPreconditionFailed):
		status, code = 412, "precondition_failed"
	case errors.Is(err, user.ErrUnavailable):
		status, code, reason = 503, "service_unavailable", "db_unavailable"
	case errors.Is(err, user.ErrTimeout):
		status, code, reason = 503, "database_timeout", "db_timeout"
	case errors.Is(err, user.ErrSchema):
		reason = "schema_missing"
	}
	if status >= 500 {
		h.logger.Error("project operation failed", "reason", reason, "status", status)
	}
	h.fail(w, r, status, code, nil)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method != "HEAD" {
		_ = json.NewEncoder(w).Encode(body)
	}
}
