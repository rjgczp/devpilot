package monitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
)

func TestHistorySelectors(t *testing.T) {
	for _, raw := range []string{"limit=0", "limit=51", "limit=-1", "limit=020", "limit=1&limit=2", "state=completed", "state=all&state=unknown", "after=", "after=bad", "extra=1", "%xy"} {
		if _, ok := historySelectors(raw, exampleID, exampleID, exampleID); ok {
			t.Fatal("accepted", raw)
		}
	}
	q, ok := historySelectors("", exampleID, exampleID, exampleID)
	if !ok || q.Limit != 20 || q.State != "all" {
		t.Fatal(q)
	}
	c := historyCursor{Version: 1, WorkspaceID: exampleID, ProjectID: exampleID, MonitorID: exampleID, State: "unknown", BeforeAt: time.Now().UTC(), BeforeID: exampleID, UpperAt: time.Now().UTC(), UpperID: exampleID}
	b, _ := json.Marshal(c)
	after := base64.RawURLEncoding.EncodeToString(b)
	if _, ok = historySelectors("limit=50&state=unknown&after="+after, exampleID, exampleID, exampleID); !ok {
		t.Fatal("valid scoped cursor")
	}
	for _, raw := range []string{"state=all&after=" + after, "state=unknown&after=" + after + "="} {
		if _, ok = historySelectors(raw, exampleID, exampleID, exampleID); ok {
			t.Fatal("unbound/noncanonical cursor")
		}
	}
	if _, ok = historySelectors("state=unknown&after="+after, "other", exampleID, exampleID); ok {
		t.Fatal("cross scope cursor")
	}
}

type historyStub struct {
	*repositoryStub
	calls int
	q     HistoryQuery
}

func (s *historyStub) History(_ context.Context, _ session.Digest, _, _, _ string, q HistoryQuery) (HistoryPage, error) {
	s.calls++
	s.q = q
	return HistoryPage{ConfigurationVersion: "1", Data: []HistoryItem{}}, nil
}
func TestHistoryHTTPTransport(t *testing.T) {
	for _, tc := range []struct {
		method, suffix, body, key, value string
		status, calls                    int
	}{
		{"GET", "/history", "", "", "", 200, 1},
		{"GET", "/history?limit=50&state=unknown", "", "", "", 200, 1},
		{"GET", "/history?limit=51", "", "", "", 400, 0},
		{"GET", "/history?limit=1&limit=2", "", "", "", 400, 0},
		{"GET", "/history?state=invalid", "", "", "", 400, 0},
		{"GET", "/history?", "", "", "", 400, 0},
		{"GET", "/history/", "", "", "", 404, 0},
		{"GET", "/history", "x", "", "", 400, 0},
		{"GET", "/history", "", "Cookie", "", 401, 0},
		{"GET", "/history", "", "If-None-Match", "x", 400, 0},
		{"POST", "/history", "", "", "", 405, 0},
		{"HEAD", "/history", "", "", "", 405, 0},
	} {
		t.Run(tc.method+tc.suffix+tc.key, func(t *testing.T) {
			s := &historyStub{repositoryStub: &repositoryStub{}}
			h := testHandler(s.repositoryStub, io.Discard)
			h.store = s
			r := request(tc.method, itemPath+tc.suffix, tc.body)
			if tc.key != "" {
				r.Header.Set(tc.key, tc.value)
			}
			w := response(t, h, r, tc.status, "")
			if s.calls != tc.calls {
				t.Fatal("unexpected DB call")
			}
			if tc.status == 200 && (w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("ETag") != "") {
				t.Fatal("read headers")
			}
			if tc.status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "GET" {
				t.Fatal("method contract")
			}
		})
	}
}
