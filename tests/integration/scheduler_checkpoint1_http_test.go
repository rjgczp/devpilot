//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
)

func checkpointHTTPServer(t *testing.T, db *pgxpool.Pool) (*httptest.Server, *http.Client, *bytes.Buffer) {
	t.Helper()
	logs := new(bytes.Buffer)
	origin, err := config.ParseOrigin("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	h := monitor.New(monitor.Store{Pool: db}, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(logs, nil)))
	s, client := checkpointHTTPHandlerServer(t, h)
	return s, client, logs
}

func checkpointHTTPHandlerServer(t *testing.T, h http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	var err error
	if address := os.Getenv("MONITOR_PROBE_HTTP_ADDR"); address != "" {
		s.Listener.Close()
		s.Listener, err = net.Listen("tcp4", address)
		if err != nil {
			t.Fatal(err)
		}
	}
	s.Start()
	t.Cleanup(s.Close)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	return s, &http.Client{Transport: transport}
}
func checkpointHTTP(t *testing.T, c *http.Client, server, method, path, body, token, tag string, modify func(http.Header)) (int, http.Header, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, server+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "__Host-devpilot_session", Value: token})
	}
	if method != "GET" {
		r.Header.Set("Origin", "https://app.example.com")
		r.Header.Set("X-DevPilot-CSRF", "1")
		r.Header.Set("Content-Type", "application/json")
	}
	if tag != "" {
		r.Header.Set("If-Match", tag)
	}
	if modify != nil {
		modify(r.Header)
	}
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(raw) > 16384 {
		t.Fatal("unbounded response", err)
	}
	return resp.StatusCode, resp.Header, raw
}
func TestCheckpointOneAuthorizationRealHTTPStrictContract(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	server, client, logs := checkpointHTTPServer(t, db)
	path := "/api/v1/workspaces/" + m.WorkspaceID + "/projects/" + m.ProjectID + "/monitors/" + m.ID + "/execution-authorization"
	a := activationRead(t, db, f.od, m)
	tag := monitor.ActivationETag(a)
	valid := `{"configuration_version":"1","consent_profile":"periodic-http-get-v1","authorization_seconds":60}`
	cases := []struct {
		name, body, token, tag string
		status                 int
		modify                 func(http.Header)
	}{
		{name: "cookie-first", body: valid, tag: "", status: 401},
		{name: "member-before-precondition", body: valid, token: f.memberToken, status: 403},
		{name: "foreign-before-precondition", body: valid, token: f.otherToken, status: 404},
		{name: "required", body: valid, token: f.ownerToken, status: 428},
		{name: "stale", body: valid, token: f.ownerToken, tag: `"monitor-activation:` + m.ID + `:1:99"`, status: 412},
	}
	invalidBodies := []string{`null`, `[]`, `{}`, valid + ` {}`, strings.TrimSuffix(valid, "}") + `,"extra":true}`, strings.TrimSuffix(valid, "}") + `,"configuration_\u0076ersion":"1"}`, strings.Replace(valid, `:60`, `:null`, 1), strings.Replace(valid, `:60`, `:"60"`, 1), strings.Replace(valid, `:60`, `:60.0`, 1), strings.Replace(valid, `:60`, `:6e1`, 1), strings.Replace(valid, `"1"`, `null`, 1), strings.Replace(valid, `"1"`, `"\ud800"`, 1), string(append([]byte(valid[:len(valid)-1]), 255, '}'))}
	for _, body := range invalidBodies {
		cases = append(cases, struct {
			name, body, token, tag string
			status                 int
			modify                 func(http.Header)
		}{name: "strict-json", body: body, token: f.ownerToken, tag: tag, status: 400})
	}
	for _, v := range []string{"0", "01", "+1", "9223372036854775808"} {
		cases = append(cases, struct {
			name, body, token, tag string
			status                 int
			modify                 func(http.Header)
		}{name: "version", body: strings.Replace(valid, `"1"`, `"`+v+`"`, 1), token: f.ownerToken, tag: tag, status: 422})
	}
	for _, h := range []struct {
		name   string
		status int
		edit   func(http.Header)
	}{
		{"weak", 400, func(h http.Header) { h.Set("If-Match", "W/"+tag) }},
		{"wildcard", 400, func(h http.Header) { h.Set("If-Match", "*") }},
		{"duplicate", 400, func(h http.Header) { h.Add("If-Match", tag) }},
		{"multivalue", 400, func(h http.Header) { h.Set("If-Match", tag+","+tag) }},
		{"media", 415, func(h http.Header) { h.Set("Content-Type", "text/plain") }},
		{"duplicate-media", 415, func(h http.Header) { h.Add("Content-Type", "application/json") }},
		{"encoding", 415, func(h http.Header) { h.Set("Content-Encoding", "identity") }},
		{"origin", 403, func(h http.Header) { h.Set("Origin", "null") }},
		{"csrf", 403, func(h http.Header) { h.Del("X-DevPilot-CSRF") }},
	} {
		cases = append(cases, struct {
			name, body, token, tag string
			status                 int
			modify                 func(http.Header)
		}{name: h.name, body: valid, token: f.ownerToken, tag: tag, status: h.status, modify: h.edit})
	}
	cases = append(cases, struct {
		name, body, token, tag string
		status                 int
		modify                 func(http.Header)
	}{name: "1025", body: valid + strings.Repeat(" ", 1025-len(valid)), token: f.ownerToken, tag: tag, status: 413})
	for i, tc := range cases {
		status, headers, raw := checkpointHTTP(t, client, server.URL, "PUT", path, tc.body, tc.token, tc.tag, tc.modify)
		if status != tc.status {
			t.Fatalf("case %d %s: HTTP %d want %d body=%s", i, tc.name, status, tc.status, raw)
		}
		if headers.Get("ETag") != "" || headers.Get("Set-Cookie") != "" || bytes.Contains(raw, []byte(`"data"`)) {
			t.Fatal("failure carried success data", tc.name)
		}
		if activationRead(t, db, f.od, m).ActivationGeneration != "1" || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1`, m.ID) != 0 {
			t.Fatal("rejected request changed authorization", tc.name)
		}
	}
	status, headers, raw := checkpointHTTP(t, client, server.URL, "PUT", path, valid+strings.Repeat(" ", 1024-len(valid)), f.ownerToken, tag, nil)
	var grant struct {
		Data monitor.ExecutionAuthorization
	}
	if status != 200 || json.Unmarshal(raw, &grant) != nil || grant.Data.ActivationGeneration != "2" || grant.Data.EffectiveAuthorization {
		t.Fatal("1024-byte confirmed grant", status, string(raw))
	}
	if headers.Get("ETag") != monitor.ActivationETag(activationRead(t, db, f.od, m)) || headers.Get("Cache-Control") != "no-store" || headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("grant response contract")
	}
	status, _, raw = checkpointHTTP(t, client, server.URL, "GET", strings.TrimSuffix(path, "execution-authorization")+"execution-status", "", f.memberToken, "", nil)
	var result struct{ Data monitor.ExecutionStatus }
	if status != 200 || json.Unmarshal(raw, &result) != nil || result.Data.AuthorizationState != "valid" || result.Data.SchedulingState != "blocked" {
		t.Fatal("Member status", status, string(raw))
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
		t.Fatal("HTTP API created a run")
	}
	if strings.Contains(logs.String(), f.ownerToken) || strings.Contains(logs.String(), "private-path") {
		t.Fatal("sensitive HTTP log")
	}
	if _, err := (monitor.Store{Pool: db}).ExecutionStatus(ctx, f.xd, m.WorkspaceID, m.ProjectID, m.ID); err == nil {
		t.Fatal("cross-tenant status visible")
	}
}
