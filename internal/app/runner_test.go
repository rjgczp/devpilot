package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunnerGracefulShutdown(t *testing.T) {
	listener := testListener(t)
	var draining atomic.Bool
	var resourceClosed atomic.Bool
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	workerStopped := make(chan struct{})
	var logs bytes.Buffer
	handler := http.NewServeMux()
	handler.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		w.WriteHeader(http.StatusOK)
	})
	handler.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if draining.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	runner := Runner{
		Server:          NewServer(listener.Addr().String(), handler),
		Logger:          NewLogger(&logs, slog.LevelInfo),
		Draining:        &draining,
		ShutdownTimeout: time.Second,
		CloseResource:   func() { resourceClosed.Store(true) },
		Workers: []func(context.Context){func(ctx context.Context) {
			<-ctx.Done()
			close(workerStopped)
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/slow")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				err = context.Canceled
			}
		}
		requestDone <- err
	}()
	waitFor(t, requestStarted)
	cancel()
	waitFor(t, workerStopped)
	if !draining.Load() {
		t.Fatal("runner did not mark itself as draining")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), 20*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(5 * time.Millisecond)
	}
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), 20*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("runner still accepts new connections during shutdown")
	}
	response := newResponse(handler, "/readyz")
	if response != http.StatusServiceUnavailable {
		t.Fatalf("readiness during shutdown: %d", response)
	}
	close(releaseRequest)
	if err := receive(t, requestDone); err != nil {
		t.Fatalf("in-flight request failed: %v", err)
	}
	if err := receive(t, result); err != nil {
		t.Fatalf("graceful shutdown failed: %v", err)
	}
	if !resourceClosed.Load() {
		t.Fatal("managed resource was not closed")
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("non-JSON operational log: %v", err)
		}
	}
	if !strings.Contains(logs.String(), "server started") || !strings.Contains(logs.String(), "server stopped") {
		t.Fatalf("missing lifecycle logs: %s", logs.String())
	}
}

func TestRunnerShutdownDeadline(t *testing.T) {
	listener := testListener(t)
	var draining atomic.Bool
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	releaseWorker := make(chan struct{})
	resourceClosed := make(chan struct{})
	var logs bytes.Buffer
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		w.WriteHeader(http.StatusOK)
	})
	runner := Runner{
		Server:          NewServer(listener.Addr().String(), handler),
		Logger:          NewLogger(&logs, slog.LevelInfo),
		Draining:        &draining,
		ShutdownTimeout: 60 * time.Millisecond,
		CloseResource:   func() { close(resourceClosed) },
		Workers: []func(context.Context){func(context.Context) {
			<-releaseWorker // Deliberately ignores cancellation.
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx, listener) }()
	clientDone := make(chan struct{})
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/")
		if err == nil {
			_ = response.Body.Close()
		}
		close(clientDone)
	}()
	waitFor(t, requestStarted)
	start := time.Now()
	cancel()
	if err := receive(t, result); err == nil {
		t.Fatal("expected non-zero shutdown result")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("shutdown waited beyond its configured deadline")
	}
	if !strings.Contains(logs.String(), "deadline_exceeded") {
		t.Fatalf("missing timeout reason: %s", logs.String())
	}
	waitFor(t, resourceClosed)
	close(releaseRequest)
	close(releaseWorker)
	waitFor(t, clientDone)
}

func TestRunnerShutdownError(t *testing.T) {
	base := testListener(t)
	listener := closeErrorListener{Listener: base}
	var draining atomic.Bool
	var logs bytes.Buffer
	runner := Runner{
		Server:          NewServer(base.Addr().String(), http.NewServeMux()),
		Logger:          NewLogger(&logs, slog.LevelInfo),
		Draining:        &draining,
		ShutdownTimeout: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx, listener) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := receive(t, result); err == nil {
		t.Fatal("expected shutdown error")
	}
	if !strings.Contains(logs.String(), "http_shutdown_error") {
		t.Fatalf("missing shutdown error reason: %s", logs.String())
	}
}

type closeErrorListener struct{ net.Listener }

func (l closeErrorListener) Close() error {
	_ = l.Listener.Close()
	return context.Canceled
}

func testListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func waitFor(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func receive(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for result")
		return nil
	}
}

func newResponse(handler http.Handler, path string) int {
	recorder := &statusRecorder{header: make(http.Header)}
	handler.ServeHTTP(recorder, mustRequest(path))
	return recorder.status
}

type statusRecorder struct {
	header http.Header
	status int
}

func (r *statusRecorder) Header() http.Header       { return r.header }
func (r *statusRecorder) Write([]byte) (int, error) { return 0, nil }
func (r *statusRecorder) WriteHeader(code int)      { r.status = code }

func mustRequest(path string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, "http://example.test"+path, nil)
	return request
}
