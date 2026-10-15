package workspace

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

// Member paths are parsed explicitly; aliases/extra segments cannot mutate data.
func (h *Handler) serveMembers(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	// Bound net/http's implicit request-body drain even for early method/CSRF
	// rejections and GET bodies that are deliberately ignored by the API.
	if deadline, ok := ctx.Deadline(); ok {
		_ = http.NewResponseController(w).SetReadDeadline(deadline)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, collection+"/"), "/")
	if (len(parts) != 2 && len(parts) != 3) || parts[0] == "" || parts[1] != "members" || r.URL.RawPath != "" || (len(parts) == 3 && parts[2] == "") {
		http.NotFound(w, r)
		return
	}
	isList := len(parts) == 2
	allow := "DELETE"
	if isList {
		allow = "GET"
	}
	if r.Method != allow {
		w.Header().Set("Allow", allow)
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	if !isList && !auth.ValidCSRF(r, h.policy) {
		h.fail(w, r, 403, "csrf_rejected", nil)
		return
	}
	digest, ok := session.CookieDigest(r, auth.CookieName(h.policy))
	if !ok {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	limit, after, ok := selectors(r.URL.RawQuery, isList)
	if !ok {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	condition := r.Header.Get("If-Match")
	if !isList {
		if len(r.Header.Values("If-Match")) > 1 || (len(r.Header.Values("If-Match")) == 1 && !validMemberCondition(condition)) {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
		empty, err := emptyMemberBody(ctx, w, r)
		if r.Context().Err() != nil {
			// A read deadline can cancel the net/http request context. Returning
			// without a response would let net/http synthesize 200; abort instead.
			panic(http.ErrAbortHandler)
		}
		if ctx.Err() != nil || errors.Is(err, user.ErrTimeout) {
			h.fail(w, r, 503, "service_unavailable", nil)
			return
		}
		if err != nil || !empty {
			h.fail(w, r, 400, "invalid_request", nil)
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
	dbctx, cancel := context.WithTimeout(ctx, h.databaseTimeout)
	defer cancel()
	var result any
	var err error
	status := 200
	if isList {
		result, err = h.store.ListMembers(dbctx, digest, parts[0], limit, after)
	} else {
		err = h.store.DeleteMember(dbctx, digest, parts[0], parts[2], condition)
		status = 204
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
	h.write(w, r, status, result)
}

// Read at most ONE byte. A live HTTP read deadline enforces the processing
// budget even when a chunked client withholds its first byte/end marker.
func emptyMemberBody(ctx context.Context, w http.ResponseWriter, r *http.Request) (bool, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return true, nil
	}
	c := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	supported := c.SetReadDeadline(deadline) == nil
	done := make(chan struct{})
	fullyRead := false
	stop := context.AfterFunc(ctx, func() {
		if supported {
			_ = c.SetReadDeadline(time.Now())
		} else {
			_ = r.Body.Close()
		}
		close(done)
	})
	defer func() {
		if !stop() {
			<-done
		}
		if supported && fullyRead {
			_ = c.SetReadDeadline(time.Time{})
		}
	}()
	var b [1]byte
	n, err := io.ReadFull(r.Body, b[:])
	if ctx.Err() != nil || (!deadline.IsZero() && !time.Now().Before(deadline)) {
		return false, user.ErrTimeout
	}
	if n > 0 {
		// Do not let net/http drain an unfinished rejected body before flushing
		// 400 or during Body.Close. This connection cannot safely be reused.
		w.Header().Set("Connection", "close")
		if supported {
			_ = c.SetReadDeadline(time.Now())
		}
		return false, nil
	}
	if errors.Is(err, io.EOF) {
		fullyRead = true
		return true, nil
	}
	return false, err
}
