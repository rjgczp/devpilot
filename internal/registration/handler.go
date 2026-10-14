package registration

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

	"github.com/rjgczp/devpilot/internal/user"
)

type Store interface {
	Create(context.Context, string, string) (user.Public, error)
}
type Hasher interface{ Hash(string) (string, error) }

type Handler struct {
	store             Store
	hasher            Hasher
	logger            *slog.Logger
	slots             chan struct{}
	processingTimeout time.Duration
	databaseTimeout   time.Duration
}

func New(store Store, hasher Hasher, logger *slog.Logger) *Handler {
	return &Handler{store: store, hasher: hasher, logger: logger, slots: make(chan struct{}, 2), processingTimeout: 5 * time.Second, databaseTimeout: 2 * time.Second}
}

type responseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *responseWriter) WriteHeader(status int) {
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}

func (h *Handler) ServeHTTP(output http.ResponseWriter, r *http.Request) {
	w := &responseWriter{ResponseWriter: output}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(http.ErrAbortHandler)
			}
			h.logger.Error("registration failed", "reason", "internal_panic")
			if w.written {
				panic(http.ErrAbortHandler)
			}
			h.fail(w, r, http.StatusInternalServerError, "internal_error", nil)
		}
	}()
	if r.Context().Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.processingTimeout)
	defer cancel()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	validMedia := err == nil && media == "application/json"
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			validMedia = false
		}
	}
	_, encoded := r.Header["Content-Encoding"]
	if !validMedia || encoded {
		h.fail(w, r, 415, "unsupported_media_type", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if r.Context().Err() != nil {
		return
	}
	if len(body) > MaxBodyBytes {
		h.fail(w, r, 413, "request_too_large", nil)
		return
	}
	if err != nil {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	in, err := parseInput(body)
	if err != nil {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	if fields := validate(&in); len(fields) != 0 {
		h.fail(w, r, 422, "validation_failed", fields)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	hash, busy, err := h.hash(in.Password)
	if r.Context().Err() != nil {
		return
	}
	if busy {
		w.Header().Set("Retry-After", "1")
		h.fail(w, r, 503, "registration_busy", nil)
		return
	}
	if err != nil {
		h.logger.Error("registration failed", "reason", "password_hash_error")
		h.fail(w, r, 500, "internal_error", nil)
		return
	}
	if h.expired(w, r, ctx) {
		return
	}
	dbctx, cancelDB := context.WithTimeout(ctx, h.databaseTimeout)
	defer cancelDB()
	u, err := h.store.Create(dbctx, in.Email, hash)
	// A canceled client must not be promised a response or a rollback.
	if r.Context().Err() != nil || errors.Is(err, user.ErrCanceled) {
		return
	}
	if err != nil {
		status, code := 500, "internal_error"
		reason := "internal_error"
		switch {
		case errors.Is(err, user.ErrConflict):
			status, code, reason = 409, "email_conflict", "email_conflict"
		case errors.Is(err, user.ErrUnavailable):
			status, code, reason = 503, "service_unavailable", "db_unavailable"
		case errors.Is(err, user.ErrTimeout):
			status, code, reason = 503, "database_timeout", "db_timeout"
		case errors.Is(err, user.ErrSchema):
			reason = "schema_missing"
		}
		h.logger.Error("registration failed", "reason", reason, "status", status)
		h.fail(w, r, status, code, nil)
		return
	}
	if ctx.Err() != nil {
		h.fail(w, r, 503, "database_timeout", nil)
		return
	}
	h.write(w, r, 201, map[string]any{"data": u})
}

func (h *Handler) hash(password string) (hash string, busy bool, err error) {
	select {
	case h.slots <- struct{}{}:
	default:
		return "", true, nil
	}
	defer func() { <-h.slots }()
	hash, err = h.hasher.Hash(password)
	return hash, false, err
}

func (h *Handler) expired(w http.ResponseWriter, r *http.Request, ctx context.Context) bool {
	if r.Context().Err() != nil {
		return true
	}
	if ctx.Err() != nil {
		h.fail(w, r, 503, "service_unavailable", nil)
		return true
	}
	return false
}

var messages = map[string]string{
	"invalid_request":        "Invalid registration request.",
	"method_not_allowed":     "Method not allowed.",
	"request_too_large":      "Request body too large.",
	"unsupported_media_type": "Unsupported media type.",
	"validation_failed":      "Invalid registration fields.",
	"email_conflict":         "Unable to register with this email.",
	"registration_busy":      "Registration is temporarily busy.",
	"service_unavailable":    "Registration could not be confirmed.",
	"database_timeout":       "Registration could not be confirmed.",
	"internal_error":         "Unable to complete registration.",
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, code string, fields map[string]string) {
	err := map[string]any{"code": code, "message": messages[code]}
	if len(fields) != 0 {
		err["fields"] = fields
	}
	h.write(w, r, status, map[string]any{"error": err})
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, body any) {
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(body)
	}
}
