package monitor

import (
	"context"
	"errors"
	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

type ExecutionAuthorizationRepository interface {
	ExecutionAuthorize(context.Context, session.Digest, string, string, string, ExecutionAuthorizationInput, string) (ExecutionAuthorization, error)
}

// The surrounding handler owns panic/cancellation and the bounded read deadline.
func (h *Handler) serveExecutionAuthorization(w http.ResponseWriter, r *http.Request, ctx context.Context, parts []string) {
	if !strings.HasPrefix(r.URL.Path, collection) || r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path || len(parts) != 6 || parts[0] == "" || parts[1] != "projects" || parts[2] == "" || parts[3] != "monitors" || parts[4] == "" || parts[5] != "execution-authorization" {
		h.fail(w, r, 404, "monitor_not_found", nil)
		return
	}
	if r.Method != "PUT" {
		w.Header().Set("Allow", "PUT")
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	write := true
	if write && !auth.ValidCSRF(r, h.policy) {
		h.fail(w, r, 403, "csrf_rejected", nil)
		return
	}
	digest, ok := session.CookieDigest(r, auth.CookieName(h.policy))
	if !ok {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	for _, k := range []string{"If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range"} {
		if _, exists := r.Header[k]; exists {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
	}
	tag := r.Header.Get("If-Match")
	if values, exists := r.Header["If-Match"]; exists {
		_, _, _, valid := activationCondition(tag)
		if !write || len(values) != 1 || !valid {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
	}
	var input ExecutionAuthorizationInput
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
	}
	limit := int64(0)
	if write {
		limit = ActivationMaxBodyBytes
	}
	b, err := readBodyLimit(ctx, w, r, limit)
	if r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
	deadline, _ := ctx.Deadline()
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		h.fail(w, r, 503, "service_unavailable", nil)
		return
	}
	if write && len(b) > ActivationMaxBodyBytes {
		h.fail(w, r, 413, "request_too_large", nil)
		return
	}
	if err != nil || !write && len(b) != 0 {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	if write {
		input, ok = parseExecutionAuthorization(b)
		if !ok {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
		if fields := validateExecutionAuthorization(input); len(fields) > 0 {
			h.fail(w, r, 422, "validation_failed", fields)
			return
		}
	}
	store, ok := h.store.(ExecutionAuthorizationRepository)
	if !ok {
		h.fail(w, r, 500, "internal_error", nil)
		return
	}
	dbctx, stop := context.WithTimeout(ctx, h.databaseTimeout)
	defer stop()
	var a ExecutionAuthorization
	a, err = store.ExecutionAuthorize(dbctx, digest, parts[0], parts[2], parts[4], input, tag)
	if r.Context().Err() != nil {
		return
	}
	if errors.Is(err, user.ErrCanceled) {
		err = user.ErrInternal
	}
	if err == nil && dbctx.Err() != nil {
		err = user.ErrTimeout
	}
	if err != nil {
		var rate ActivationRateLimit
		switch {
		case errors.Is(err, ErrRetentionCapacity):
			h.fail(w, r, 409, "retention_capacity", nil)
		case errors.Is(err, ErrActivationLimit):
			h.fail(w, r, 409, "activation_limit_reached", nil)
		case errors.As(err, &rate):
			w.Header().Set("Retry-After", strconv.Itoa(rate.RetryAfter))
			h.fail(w, r, 429, "activation_rate_limited", nil)
		default:
			h.operationError(w, r, err)
		}
		return
	}
	w.Header().Set("ETag", ActivationETag(Activation{MonitorID: a.MonitorID, ConfigurationVersion: a.ConfigurationVersion, ActivationGeneration: a.ActivationGeneration}))
	h.write(w, r, 200, struct {
		Data ExecutionAuthorization `json:"data"`
	}{a})
}
