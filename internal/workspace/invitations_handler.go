package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func invitationPath(s string) bool {
	return s == "/api/v1/invitations" || strings.HasPrefix(s, "/api/v1/invitations/") || (strings.HasPrefix(path.Clean(s), collection+"/") && strings.Contains(strings.TrimPrefix(s, collection+"/"), "/invitations"))
}
func invitationSelectors(raw string) (int, string, string, bool) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return 0, "", "", false
	}
	filter := "pending"
	if v, ok := q["status"]; ok {
		if len(v) != 1 || (v[0] != "pending" && v[0] != "all") {
			return 0, "", "", false
		}
		filter = v[0]
		q.Del("status")
	}
	n, after, ok := selectors(q.Encode(), true)
	return n, after, filter, ok
}
func parseInvitee(b []byte) (string, bool) {
	if !credentials.ValidJSON(b) {
		return "", false
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') || !d.More() {
		return "", false
	}
	tok, err = d.Token()
	if err != nil || tok != "invitee_user_id" {
		return "", false
	}
	var raw json.RawMessage
	if d.Decode(&raw) != nil || len(raw) == 0 || raw[0] != '"' || d.More() {
		return "", false
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return v, true
}
func (h *Handler) serveInvitations(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = http.NewResponseController(w).SetReadDeadline(deadline)
	}
	if r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path {
		h.fail(w, r, 404, "invitation_not_found", nil)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	wid, iid, action := "", "", ""
	owner, list := false, false
	allow := ""
	switch {
	case len(parts) == 3 && parts[0] == "workspaces" && parts[1] != "" && parts[2] == "invitations":
		wid = parts[1]
		owner = true
		allow = "GET, POST"
		list = r.Method == "GET"
		action = "create"
	case len(parts) == 1 && parts[0] == "invitations":
		allow = "GET"
		list = true
	case len(parts) == 2 && parts[0] == "invitations" && parts[1] != "":
		iid = parts[1]
		allow = "DELETE"
		action = "revoke"
	case len(parts) == 3 && parts[0] == "invitations" && parts[1] != "" && (parts[2] == "accept" || parts[2] == "decline"):
		iid = parts[1]
		allow = "POST"
		action = parts[2]
	default:
		h.fail(w, r, 404, "invitation_not_found", nil)
		return
	}
	if !(r.Method == allow || (allow == "GET, POST" && (r.Method == "GET" || r.Method == "POST"))) {
		w.Header().Set("Allow", allow)
		h.fail(w, r, 405, "method_not_allowed", nil)
		return
	}
	if !list && !auth.ValidCSRF(r, h.policy) {
		h.fail(w, r, 403, "csrf_rejected", nil)
		return
	}
	d, ok := session.CookieDigest(r, auth.CookieName(h.policy))
	if !ok {
		h.fail(w, r, 401, "unauthenticated", nil)
		return
	}
	n, after, filter := 0, "", ""
	if list {
		n, after, filter, ok = invitationSelectors(r.URL.RawQuery)
	} else {
		_, _, ok = selectors(r.URL.RawQuery, false)
	}
	if !ok {
		h.fail(w, r, 400, "invalid_request", nil)
		return
	}
	target := ""
	if !list && action == "create" {
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		ok = err == nil && media == "application/json" && len(r.Header.Values("Content-Type")) == 1
		for k, v := range params {
			if k != "charset" || !strings.EqualFold(v, "utf-8") {
				ok = false
			}
		}
		if _, encoded := r.Header["Content-Encoding"]; encoded {
			ok = false
		}
		if !ok {
			h.fail(w, r, 415, "unsupported_media_type", nil)
			return
		}
		b, err := readInvitationBody(ctx, w, r)
		if r.Context().Err() != nil {
			panic(http.ErrAbortHandler)
		}
		if deadline, ok := ctx.Deadline(); ctx.Err() != nil || (ok && !time.Now().Before(deadline)) {
			h.fail(w, r, 503, "service_unavailable", nil)
			return
		}
		if len(b) > credentials.MaxBodyBytes {
			h.fail(w, r, 413, "request_too_large", nil)
			return
		}
		target, ok = parseInvitee(b)
		if err != nil || !ok {
			h.fail(w, r, 400, "invalid_request", nil)
			return
		}
	} else if !list {
		empty, err := emptyMemberBody(ctx, w, r)
		if r.Context().Err() != nil {
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
	repo, ok := h.store.(InvitationRepository)
	if !ok {
		h.operationError(w, r, user.ErrInternal)
		return
	}
	dbctx, cancel := context.WithTimeout(ctx, h.databaseTimeout)
	defer cancel()
	var result any
	var err error
	status := 200
	switch {
	case list:
		result, err = repo.ListInvitations(dbctx, d, wid, owner, n, after, filter)
	case action == "create":
		var v OwnerInvitation
		v, err = repo.CreateInvitation(dbctx, d, wid, target)
		result = map[string]any{"data": v}
		status = 201
	default:
		var v Acceptance
		v, err = repo.ActOnInvitation(dbctx, d, iid, action)
		result = map[string]any{"data": v}
		if action != "accept" {
			status = 204
		}
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

// A response wrapper may not expose transport deadlines. Cancellation must
// still close its body; wait for the callback before resetting a live deadline.
func readInvitationBody(ctx context.Context, w http.ResponseWriter, r *http.Request) ([]byte, error) {
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
	b, err := io.ReadAll(io.LimitReader(r.Body, credentials.MaxBodyBytes+1))
	if !stop() {
		<-done
	}
	if len(b) > credentials.MaxBodyBytes || err != nil || ctx.Err() != nil {
		w.Header().Set("Connection", "close")
		if supported {
			_ = c.SetReadDeadline(time.Now())
		}
	} else if supported {
		_ = c.SetReadDeadline(deadline)
	}
	return b, err
}
