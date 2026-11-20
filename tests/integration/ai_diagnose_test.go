//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/ai"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
)

// Uses only the guarded disposable PG18 test database. The provider is a local
// mock and no monitored target is resolved or called.
func TestAIDiagnoseWorkspaceBoundaryPG(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	var modelCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer isolated-mock-key" {
			t.Error("server key")
		}
		result := `{"summary":"没有监控结果","evidence":[],"possible_causes":[],"confidence":"low","limitations":["无近期结果"],"recommendations":["检查现有管理配置"]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": result}, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := ai.New(monitor.Store{Pool: db}, config.AuthConfig{Mode: "production", Origin: origin}, config.AIConfig{BaseURL: provider.URL + "/v1", Model: "mock", APIKey: "isolated-mock-key", Timeout: time.Second})
	call := func(token, wid, pid, mid string, want int) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"workspace_id": wid, "project_id": pid, "monitor_id": mid})
		r := httptest.NewRequest("POST", "/api/v1/ai/diagnose", strings.NewReader(string(body)))
		r.Header.Set("Origin", "https://app.example.com")
		r.Header.Set("X-DevPilot-CSRF", "1")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Cookie", "__Host-devpilot_session="+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("status %d want%d: %s", w.Code, want, w.Body.String())
		}
		if want != 200 && strings.Contains(w.Body.String(), m.ID) {
			t.Fatal("monitor leaked")
		}
	}
	// Existing workspace member can read its own monitor; outsider and mixed
	// workspace tuples fail before any model call, even with a valid session.
	call(f.otherToken, m.WorkspaceID, m.ProjectID, m.ID, 404)
	call(f.memberToken, f.z.ID, m.ProjectID, m.ID, 404)
	if modelCalls.Load() != 0 {
		t.Fatal("unauthorized model call")
	}
	call(f.memberToken, m.WorkspaceID, m.ProjectID, m.ID, 200)
	if modelCalls.Load() != 1 {
		t.Fatal("member diagnosis missing")
	}
	// Revocation is rechecked by the existing business layer on every request.
	if _, e := db.Exec(t.Context(), `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, m.WorkspaceID, f.member.ID); e != nil {
		t.Fatal(e)
	}
	call(f.memberToken, m.WorkspaceID, m.ProjectID, m.ID, 404)
	if modelCalls.Load() != 1 {
		t.Fatal("revoked member model call")
	}
	var gate string
	var stopped bool
	if e := db.QueryRow(t.Context(), `SELECT gate,emergency_stop FROM scheduler_control WHERE control_id='deployment'`).Scan(&gate, &stopped); e != nil || gate != "closed" || !stopped {
		t.Fatal("execution gate changed", e)
	}
}
