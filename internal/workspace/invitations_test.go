package workspace

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
)

type invitationStub struct {
	repositoryStub
	fault       error
	createInput string
	action      func(context.Context) (Acceptance, error)
}

func (s *invitationStub) CreateInvitation(ctx context.Context, d session.Digest, wid, target string) (OwnerInvitation, error) {
	s.calls.Add(1)
	s.createInput = target
	return OwnerInvitation{exampleID, exampleID, exampleID, exampleID, "pending", time.Unix(0, 0).UTC(), time.Unix(0, 0).UTC(), time.Unix(604800, 0).UTC()}, s.fault
}
func (s *invitationStub) ListInvitations(ctx context.Context, d session.Digest, wid string, owner bool, n int, after, filter string) (InvitationPage, error) {
	s.calls.Add(1)
	p := InvitationPage{Data: []any{}}
	return p, s.fault
}
func (s *invitationStub) ActOnInvitation(ctx context.Context, d session.Digest, iid, action string) (Acceptance, error) {
	s.calls.Add(1)
	if s.action != nil {
		return s.action(ctx)
	}
	return Acceptance{exampleID, exampleID, exampleID, "member", time.Unix(0, 0).UTC()}, s.fault
}
func invitationHandler(s *invitationStub) *Handler {
	h := testHandler(&s.repositoryStub, io.Discard)
	h.store = s
	return h
}
func TestInvitationInputProtocol(t *testing.T) {
	base := collection + "/" + exampleID + "/invitations"
	for _, b := range []string{`null`, `[]`, `{}`, `{"invitee_user_id":null}`, `{"invitee_user_id":1}`, `{"invitee_user_id":"x","invitee_user_id":"y"}`, `{"invitee_user_id":"x","role":"owner"}`, `{"invitee_user_id":"\ud800"}`, `{"invitee_user_id":"x"} {}`, "{\"invitee_user_id\":\"\xff\"}"} {
		s := &invitationStub{}
		response(t, invitationHandler(s), request("POST", base, b), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("invalid input reached database", b)
		}
	}
	s := &invitationStub{}
	body := `{"invitee_user_id":"` + exampleID + `"}`
	response(t, invitationHandler(s), request("POST", base, body), 201, "")
	if s.createInput != exampleID {
		t.Fatal("target changed")
	}
	for _, b := range []string{"x", " ", "null", "{}"} {
		s = &invitationStub{}
		response(t, invitationHandler(s), request("POST", "/api/v1/invitations/"+exampleID+"/accept", b), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("nonempty accept")
		}
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=01", "after=", "after=BAD", "status=expired", "status=all&status=pending", "user_id=" + exampleID, "status=%xx"} {
		s = &invitationStub{}
		response(t, invitationHandler(s), request("GET", "/api/v1/invitations?"+q, ""), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("selector queried")
		}
	}
	for _, q := range []string{"", "?limit=1&status=all", "?limit=100&status=pending&after=" + exampleID} {
		s = &invitationStub{}
		response(t, invitationHandler(s), request("GET", "/api/v1/invitations"+q, "ignored"), 200, "")
	}
	for _, ct := range []string{"text/plain", "application/json; charset=ascii"} {
		s = &invitationStub{}
		r := request("POST", base, body)
		r.Header.Set("Content-Type", ct)
		response(t, invitationHandler(s), r, 415, "unsupported_media_type")
	}
	s = &invitationStub{}
	response(t, invitationHandler(s), request("POST", base, strings.Repeat(" ", 4097)), 413, "request_too_large")
}
func TestInvitationMethodsAndCSRF(t *testing.T) {
	routes := map[string]string{collection + "/" + exampleID + "/invitations": "GET, POST", "/api/v1/invitations": "GET", "/api/v1/invitations/" + exampleID: "DELETE", "/api/v1/invitations/" + exampleID + "/accept": "POST", "/api/v1/invitations/" + exampleID + "/decline": "POST"}
	for path, allow := range routes {
		for _, method := range []string{"GET", "POST", "DELETE", "PATCH", "PUT", "HEAD", "OPTIONS"} {
			if strings.Contains(allow, method) {
				continue
			}
			s := &invitationStub{}
			w := response(t, invitationHandler(s), request(method, path, "{}"), 405, "method_not_allowed")
			if w.Header().Get("Allow") != allow || s.calls.Load() != 0 {
				t.Fatal("method gate")
			}
		}
	}
	for path, method := range map[string]string{collection + "/" + exampleID + "/invitations": "POST", "/api/v1/invitations/" + exampleID: "DELETE", "/api/v1/invitations/" + exampleID + "/accept": "POST", "/api/v1/invitations/" + exampleID + "/decline": "POST"} {
		for _, header := range []string{"Origin", "X-DevPilot-CSRF", "Sec-Fetch-Site"} {
			s := &invitationStub{}
			r := request(method, path, "")
			r.Header.Set(header, "bad")
			response(t, invitationHandler(s), r, 403, "csrf_rejected")
			if s.calls.Load() != 0 {
				t.Fatal("CSRF queried")
			}
		}
		s := &invitationStub{}
		r := request(method, path, "")
		r.Header.Del("Cookie")
		response(t, invitationHandler(s), r, 401, "unauthenticated")
		if s.calls.Load() != 0 {
			t.Fatal("bad cookie queried")
		}
	}
	for _, path := range []string{"/api/v1/invitations//" + exampleID + "/accept", "/api/v1/invitations/" + exampleID + "/accept/", "/api/v1/invitations/../invitations/" + exampleID + "/accept", collection + "/" + exampleID + "/invitations/extra"} {
		s := &invitationStub{}
		w := httptest.NewRecorder()
		invitationHandler(s).ServeHTTP(w, request("POST", path, ""))
		if w.Code != 404 || w.Header().Get("Location") != "" || s.calls.Load() != 0 {
			t.Fatal("alias mutated", path, w.Code)
		}
	}
}
func TestInvitationResponseContracts(t *testing.T) {
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		t.Fatal("openapi")
	}
	paths := doc["paths"].(map[string]any)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"POST", collection + "/{workspace_id}/invitations", `{"invitee_user_id":"` + exampleID + `"}`, 201}, {"GET", collection + "/{workspace_id}/invitations", "", 200}, {"GET", "/api/v1/invitations", "", 200}, {"POST", "/api/v1/invitations/{invitation_id}/accept", "", 200}, {"POST", "/api/v1/invitations/{invitation_id}/decline", "", 204}, {"DELETE", "/api/v1/invitations/{invitation_id}", "", 204}} {
		s := &invitationStub{}
		url := strings.ReplaceAll(strings.ReplaceAll(tc.path, "{workspace_id}", exampleID), "{invitation_id}", exampleID)
		w := response(t, invitationHandler(s), request(tc.method, url, tc.body), tc.status, "")
		spec := paths[tc.path].(map[string]any)[strings.ToLower(tc.method)].(map[string]any)["responses"].(map[string]any)
		if tc.status != 204 {
			var actual any
			json.Unmarshal(w.Body.Bytes(), &actual)
			key := "200"
			if tc.status == 201 {
				key = "201"
			}
			validateContract(t, doc, spec[key].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any), actual)
		}
	}
	for code, status := range invitationStatuses {
		s := &invitationStub{fault: code}
		method, path, body := "POST", "/api/v1/invitations/"+exampleID+"/accept", ""
		if code == ErrInviteeUnavailable || code == ErrInvitationForbidden || code == ErrInvitationPending || code == ErrInvitationLimit {
			path = collection + "/" + exampleID + "/invitations"
			body = `{"invitee_user_id":"` + exampleID + `"}`
		}
		response(t, invitationHandler(s), request(method, path, body), status, string(code))
		if s.calls.Load() != 1 {
			t.Fatal("business error replay")
		}
	}
}
func TestMemberConditions(t *testing.T) {
	valid := MemberCondition(exampleID, "member")
	if !validMemberCondition(valid) {
		t.Fatal("valid condition")
	}
	for _, v := range []string{"", "*", "W/" + valid, valid + "," + valid, " " + valid, strings.ToUpper(valid), MemberCondition(exampleID, "admin"), `"membership:bad:member"`} {
		if validMemberCondition(v) {
			t.Fatal("accepted invalid validator", v)
		}
	}
	path := collection + "/" + exampleID + "/members/" + exampleID
	for _, v := range []string{"*", "W/" + valid, valid + "," + valid, " " + valid, MemberCondition(exampleID, "admin")} {
		s := &repositoryStub{}
		r := request("DELETE", path, "")
		r.Header.Set("If-Match", v)
		response(t, testHandler(s, io.Discard), r, 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("bad condition queried")
		}
	}
	s := &repositoryStub{}
	r := request("DELETE", path, "")
	r.Header.Add("If-Match", valid)
	response(t, testHandler(s, io.Discard), r, 400, "invalid_request")
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{ErrPreconditionRequired, 428, "precondition_required"}, {ErrPreconditionFailed, 412, "precondition_failed"}} {
		s := &repositoryStub{remove: func(context.Context, session.Digest, string, string) error { return tc.err }}
		r := request(http.MethodDelete, path, "")
		if tc.status == 428 {
			r.Header.Del("If-Match")
		}
		response(t, testHandler(s, io.Discard), r, tc.status, tc.code)
	}
}

func TestInvitationSlowBodiesAreBounded(t *testing.T) {
	for _, tc := range []struct{ method, path string }{{"POST", collection + "/" + exampleID + "/invitations"}, {"POST", "/api/v1/invitations/" + exampleID + "/accept"}, {"POST", "/api/v1/invitations/" + exampleID + "/decline"}, {"DELETE", "/api/v1/invitations/" + exampleID}} {
		t.Run(tc.path, func(t *testing.T) {
			s := &invitationStub{}
			h := invitationHandler(s)
			h.processingTimeout = 60 * time.Millisecond
			server := httptest.NewServer(h)
			defer server.Close()
			conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			_, err = io.WriteString(conn, tc.method+" "+tc.path+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err == nil {
				defer res.Body.Close()
				if res.StatusCode != 503 {
					t.Fatal("slow body status", res.StatusCode)
				}
			} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("handler left body unbounded")
			}
			if time.Since(start) > 500*time.Millisecond || s.calls.Load() != 0 {
				t.Fatal("slow body queried or exceeded budget")
			}
		})
	}
}

func TestInvitationClientDisconnectCancelsAction(t *testing.T) {
	entered, finished := make(chan struct{}), make(chan struct{})
	s := &invitationStub{action: func(ctx context.Context) (Acceptance, error) {
		close(entered)
		<-ctx.Done()
		close(finished)
		return Acceptance{}, ctx.Err()
	}}
	h := invitationHandler(s)
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(conn, "POST /api/v1/invitations/"+exampleID+"/accept HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nContent-Length: 0\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		conn.Close()
		t.Fatal("action not reached")
	}
	conn.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel business context")
	}
}

func TestInvitationCreateCancellationWithoutTransportDeadlines(t *testing.T) {
	s := &invitationStub{}
	h := invitationHandler(s)
	h.processingTimeout = 40 * time.Millisecond
	reader, writer := io.Pipe()
	defer writer.Close()
	r := request("POST", collection+"/"+exampleID+"/invitations", "")
	r.Body = reader
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, r); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unsupported read deadline left create body blocked")
	}
	if w.Code != 503 || s.calls.Load() != 0 {
		t.Fatal("body cancellation response", w.Code)
	}
}
