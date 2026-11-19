package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
)

const finalJSON = `{"summary":"Observed HTTP failure","evidence":["run-1: 503"],"possible_causes":["Upstream unavailable"],"confidence":"medium","limitations":["Stored results only"],"recommendations":["Review upstream logs manually"]}`

func modelReply(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}}})
}
func mockClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return NewClient(config.AIConfig{BaseURL: s.URL + "/v1", Model: "mock", APIKey: "server-secret", Timeout: time.Second})
}
func TestReadOnlyToolLoopAndBudget(t *testing.T) {
	var calls atomic.Int32
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer server-secret" {
			t.Error("configured endpoint/key")
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "secret-path") || strings.Contains(string(b), "secret-name") {
			t.Error("private config leaked")
		}
		var req map[string]any
		_ = json.Unmarshal(b, &req)
		if req["max_tokens"] != float64(MaxOutputTokens) {
			t.Error("token budget")
		}
		n := calls.Add(1)
		if n == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"get_monitor","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"get_recent_results","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		if !strings.Contains(string(b), `"tool_call_id":"b"`) {
			t.Error("tool evidence missing")
		}
		modelReply(w, finalJSON)
	})
	r, e := c.Diagnose(context.Background(), monitor.Public{Summary: monitor.Summary{Name: "secret-name"}, URL: "https://example.com/secret-path"}, monitor.HistoryPage{Data: []monitor.HistoryItem{}})
	if e != nil || calls.Load() != 2 || r.Confidence != "low" || len(r.Limitations) != 3 {
		t.Fatalf("result=%+v err=%v calls=%d", r, e, calls.Load())
	}
}
func TestModelFailureAndToolRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"provider_error", `PRIVATE-PROVIDER-KEY`, 500},
		{"invalid_json", `oops`, 200},
		{"truncated", `{"choices":[{"message":{"content":"{}"},"finish_reason":"length"}]}`, 200},
		{"oversized", strings.Repeat("x", MaxProviderBytes+1), 200},
		{"arbitrary_url", `{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":"fetch_url","arguments":"{\"url\":\"http://169.254.169.254\"}"}}]}}]}`, 200},
		{"cross_monitor_args", `{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":"get_monitor","arguments":"{\"monitor_id\":\"other\"}"}}]}}]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				n.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, e := c.Diagnose(context.Background(), monitor.Public{}, monitor.HistoryPage{})
			if e != ErrProvider || n.Load() != 1 {
				t.Fatal(e, n.Load())
			}
		})
	}
}
func TestLoopEndsAtThreeRequests(t *testing.T) {
	var n atomic.Int32
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		step := n.Add(1)
		b, _ := io.ReadAll(r.Body)
		if step == 3 && !strings.Contains(string(b), `"tool_choice":"none"`) {
			t.Error("last step must disable tools")
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"%d","type":"function","function":{"name":"get_monitor","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, step)
	})
	_, e := c.Diagnose(context.Background(), monitor.Public{}, monitor.HistoryPage{})
	if e != ErrProvider || n.Load() != 3 {
		t.Fatal(e, n.Load())
	}
}
func TestProviderTimeoutAndRedirect(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(150 * time.Millisecond):
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		_, e := c.Diagnose(ctx, monitor.Public{}, monitor.HistoryPage{})
		if e != context.DeadlineExceeded {
			t.Fatal(e)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var n atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
		defer target.Close()
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
		_, e := c.Diagnose(context.Background(), monitor.Public{}, monitor.HistoryPage{})
		if e != ErrProvider || n.Load() != 0 {
			t.Fatal("redirect followed", e)
		}
	})
}
