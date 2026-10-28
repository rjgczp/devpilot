package monitor

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

type ExecutionStatusRepository interface {
	ExecutionStatus(context.Context, session.Digest, string, string, string) (ExecutionStatus, error)
}

func (h *Handler) serveExecutionStatus(w http.ResponseWriter, r *http.Request, ctx context.Context, parts []string) {
	if !strings.HasPrefix(r.URL.Path, collection) || r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path || len(parts) != 6 || parts[0] == "" || parts[1] != "projects" || parts[2] == "" || parts[3] != "monitors" || parts[4] == "" || parts[5] != "execution-status" {
		h.fail(w, r, 404, "monitor_not_found", nil)
		return
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		h.fail(w, r, 405, "method_not_allowed", nil)
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
	for _, key := range []string{"If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range"} {
		if _, exists := r.Header[key]; exists {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
	}
	b, err := readBodyLimit(ctx, w, r, 0)
	if r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
	deadline, _ := ctx.Deadline()
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		h.fail(w, r, 503, "service_unavailable", nil)
		return
	}
	if err != nil || len(b) != 0 {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	store, ok := h.store.(ExecutionStatusRepository)
	if !ok {
		h.fail(w, r, 500, "internal_error", nil)
		return
	}
	dbctx, cancel := context.WithTimeout(ctx, h.databaseTimeout)
	defer cancel()
	a, err := store.ExecutionStatus(dbctx, digest, parts[0], parts[2], parts[4])
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
		h.operationError(w, r, err)
		return
	}
	h.write(w, r, 200, struct {
		Data ExecutionStatus `json:"data"`
	}{a})
}
