package workspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func TestMemberDeleteHTTPContract(t *testing.T) {
	path := collection + "/" + exampleID + "/members/" + exampleID
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(b, &doc)
	op := doc["paths"].(map[string]any)[collection+"/{workspace_id}/members/{user_id}"].(map[string]any)
	if len(op) != 1 || op["delete"] == nil {
		t.Fatal("unexpected member item method")
	}
	specs := op["delete"].(map[string]any)["responses"].(map[string]any)
	for _, tc := range []struct {
		status int
		code   string
		err    error
	}{
		{204, "", nil}, {401, "unauthenticated", session.ErrInvalid}, {403, "membership_forbidden", ErrMemberForbidden},
		{404, "workspace_not_found", ErrNotFound}, {404, "member_not_found", ErrMemberNotFound}, {409, "last_owner_required", ErrLastOwner},
		{503, "service_unavailable", user.ErrUnavailable}, {503, "database_timeout", user.ErrTimeout},
		{500, "internal_error", user.ErrSchema}, {500, "internal_error", errors.New("SENSITIVE UUID email cookie PHC DSN SQL")},
	} {
		t.Run(tc.code+strconv.Itoa(tc.status), func(t *testing.T) {
			var logs bytes.Buffer
			s := &repositoryStub{remove: func(ctx context.Context, d session.Digest, wid, target string) error {
				dl, ok := ctx.Deadline()
				if !ok || time.Until(dl) > 2*time.Second || wid != exampleID || target != exampleID {
					t.Fatal("trusted scoped inputs/budget")
				}
				return tc.err
			}}
			w := response(t, testHandler(s, &logs), request("DELETE", path, ""), tc.status, tc.code)
			if s.calls.Load() != 1 || w.Header().Get("Location") != "" || strings.Contains(logs.String()+w.Body.String(), "SENSITIVE") {
				t.Fatal("unsafe failure or automatic retry")
			}
			spec := specs[strconv.Itoa(tc.status)].(map[string]any)
			if tc.status == 204 {
				if spec["content"] != nil {
					t.Fatal("204 schema has JSON")
				}
				return
			}
			var actual any
			json.Unmarshal(w.Body.Bytes(), &actual)
			validateContract(t, doc, spec["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any), actual)
		})
	}
	for _, body := range []string{" ", "null", `{"user_id":"other"}`, strings.Repeat("x", 5000)} {
		s := &repositoryStub{}
		response(t, testHandler(s, io.Discard), request("DELETE", path, body), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("body reached db")
		}
	}
	for _, q := range []string{"?user_id=other", "?role=owner", "?limit=1", "?x=%zz"} {
		s := &repositoryStub{}
		response(t, testHandler(s, io.Discard), request("DELETE", path+q, ""), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("query reached db")
		}
	}
	for _, method := range []string{"GET", "POST", "HEAD", "OPTIONS", "PUT", "PATCH"} {
		s := &repositoryStub{}
		w := response(t, testHandler(s, io.Discard), request(method, path, ""), 405, "method_not_allowed")
		if w.Header().Get("Allow") != "DELETE" || s.calls.Load() != 0 {
			t.Fatal("method/side effect")
		}
	}
}

func TestMemberCSRFIdentityAndIsolation(t *testing.T) {
	path := collection + "/" + exampleID + "/members/" + exampleID
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Set("Origin", "null") },
		func(r *http.Request) { r.Header.Add("Origin", "https://app.example.com") }, func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com.evil.test") },
		func(r *http.Request) { r.Header.Set("Origin", "https://sub.app.example.com") }, func(r *http.Request) { r.Header.Set("Origin", "http://app.example.com") },
		func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com:444") }, func(r *http.Request) { r.Header.Set("Origin", "https://app.example.com/path") },
		func(r *http.Request) {
			r.Header.Del("Origin")
			r.Header.Set("Referer", "https://app.example.com")
			r.Host = "app.example.com"
			r.Header.Set("Forwarded", "host=app.example.com;proto=https")
		},
		func(r *http.Request) { r.Header.Del("X-DevPilot-CSRF") }, func(r *http.Request) { r.Header.Add("X-DevPilot-CSRF", "1") },
		func(r *http.Request) { r.Header.Set("X-DevPilot-CSRF", "0") }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") },
		func(r *http.Request) { r.Header["Sec-Fetch-Site"] = []string{"same-origin", "same-origin"} },
	} {
		s := &repositoryStub{}
		r := request("DELETE", path, "")
		change(r)
		response(t, testHandler(s, io.Discard), r, 403, "csrf_rejected")
		if s.calls.Load() != 0 {
			t.Fatal("CSRF reached DB")
		}
	}
	for _, method := range []string{"GET", "DELETE"} {
		route := strings.TrimSuffix(path, "/"+exampleID)
		if method == "DELETE" {
			route = path
		}
		for _, cookie := range []string{"", "__Host-devpilot_session=bad", "__Host-devpilot_session= " + strings.Repeat("A", 43), "__Host-devpilot_session=" + strings.Repeat("A", 43) + " ", "devpilot_session=" + strings.Repeat("A", 43), "__Host-devpilot_session=" + strings.Repeat("A", 43) + "; __Host-devpilot_session=" + strings.Repeat("A", 43)} {
			s := &repositoryStub{}
			r := request(method, route, "")
			r.Header.Set("Cookie", cookie)
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("A", 43))
			response(t, testHandler(s, io.Discard), r, 401, "unauthenticated")
			if s.calls.Load() != 0 {
				t.Fatal("alternative credential")
			}
		}
	}
	h := testHandler(&repositoryStub{}, io.Discard)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := request("DELETE", path, "")
			status, code := 204, ""
			if i%2 == 0 {
				r.Header.Del("Origin")
				status, code = 403, "csrf_rejected"
			}
			response(t, h, r, status, code)
		}(i)
	}
	wg.Wait()
}

func TestMemberCancellationBudgetsAndPanic(t *testing.T) {
	path := collection + "/" + exampleID + "/members/" + exampleID
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request("DELETE", path, "").WithContext(ctx))
	if s.calls.Load() != 0 || w.Body.Len() != 0 {
		t.Fatal("canceled work")
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	r := request("DELETE", path, "")
	r.Body = reader
	h.processingTimeout = 20 * time.Millisecond
	response(t, h, r, 503, "service_unavailable")
	if s.calls.Load() != 0 {
		t.Fatal("slow body database work")
	}
	s.remove = func(ctx context.Context, _ session.Digest, _, _ string) error { <-ctx.Done(); return user.ErrTimeout }
	h.processingTimeout = time.Second
	h.databaseTimeout = time.Millisecond
	response(t, h, request("DELETE", path, ""), 503, "database_timeout")
	var logs bytes.Buffer
	h = testHandler(s, &logs)
	s.remove = func(context.Context, session.Digest, string, string) error {
		panic("SENSITIVE UUID token digest PHC SQL")
	}
	response(t, h, request("DELETE", path, ""), 500, "internal_error")
	if strings.Contains(logs.String(), "SENSITIVE") {
		t.Fatal("panic log")
	}
	s.remove = func(context.Context, session.Digest, string, string) error { return ErrLastOwner }
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Fatal("post write panic")
			}
		}()
		h.ServeHTTP(panicWriter{httptest.NewRecorder()}, request("DELETE", path, ""))
	}()
}

func TestMemberSlowChunkedBodyIsBounded(t *testing.T) {
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	h.processingTimeout = 30 * time.Millisecond
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	path := collection + "/" + exampleID + "/members/" + exampleID
	_, err = io.WriteString(conn, "DELETE "+path+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nTransfer-Encoding: chunked\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Fatalf("slow body returned false success %d", resp.StatusCode)
		}
	}
	if time.Since(start) > 500*time.Millisecond || s.calls.Load() != 0 {
		t.Fatal("unbounded slow body/DB work")
	}
}

func TestMemberRejectedChunkedBodyDoesNotDrain(t *testing.T) {
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	path := collection + "/" + exampleID + "/members/" + exampleID
	_, err = io.WriteString(conn, "DELETE "+path+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nTransfer-Encoding: chunked\r\n\r\n1\r\nx\r\n")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal("rejected body still drained the unfinished request")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 || time.Since(start) > 500*time.Millisecond || s.calls.Load() != 0 {
		t.Fatal("rejected body budget/status/DB")
	}
}

func TestMemberEarlyRejectionBodyIsBounded(t *testing.T) {
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	h.processingTimeout = 30 * time.Millisecond
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	path := collection + "/" + exampleID + "/members/" + exampleID
	_, err = io.WriteString(conn, "DELETE "+path+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://evil.test\r\nContent-Length: 1\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal("early CSRF response was not bounded")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 || time.Since(start) > 500*time.Millisecond || s.calls.Load() != 0 {
		t.Fatal("early rejection budget/side effect")
	}
}
