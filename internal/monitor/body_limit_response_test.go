package monitor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A deterministic transport cancellation barrier: a past read deadline may
// make net/http's background reader cancel Request.Context before a response is
// committed. This is a unit model, not a claim about the exact failing CI trace.
type cancelOnImmediateDeadline struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *cancelOnImmediateDeadline) SetReadDeadline(deadline time.Time) error {
	if !deadline.After(time.Now()) {
		w.cancel()
	}
	return nil
}

func TestMonitorOversizedBodyCommitsErrorBeforeTransportCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		size, status       int
	}{
		{"configuration", "POST", listPath, MaxBodyBytes + 1, http.StatusRequestEntityTooLarge},
		{"activation", "PUT", itemPath + "/activation", ActivationMaxBodyBytes + 1, http.StatusRequestEntityTooLarge},
		{"execution-authorization", "PUT", itemPath + "/execution-authorization", ActivationMaxBodyBytes + 1, http.StatusRequestEntityTooLarge},
		{"execution-status", "GET", itemPath + "/execution-status", 1, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := request(tc.method, tc.path, strings.Repeat(" ", tc.size)).WithContext(ctx)
			recorder := httptest.NewRecorder()
			w := &cancelOnImmediateDeadline{ResponseRecorder: recorder, cancel: cancel}
			store := &repositoryStub{}
			h := testHandler(store, io.Discard)
			var recovered any
			func() { defer func() { recovered = recover() }(); h.ServeHTTP(w, r) }()
			if recovered != nil {
				t.Fatalf("transport cancellation prevented bounded error response: %v", recovered)
			}
			if recorder.Code != tc.status || recorder.Body.Len() == 0 || recorder.Header().Get("Connection") != "close" {
				t.Fatalf("got HTTP %d body=%q connection=%q; want bounded HTTP %d", recorder.Code, recorder.Body.String(), recorder.Header().Get("Connection"), tc.status)
			}
			if ctx.Err() == nil {
				t.Fatal("remaining transport reads were not interrupted after the error response")
			}
			if store.calls.Load() != 0 || recorder.Header().Get("ETag") != "" || recorder.Header().Get("Set-Cookie") != "" {
				t.Fatal("rejected request reached Store or carried success metadata")
			}
		})
	}
}
