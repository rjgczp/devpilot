package session

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/app"
	"github.com/rjgczp/devpilot/internal/user"
)

type cleanerFunc func(context.Context) (int64, error)

func (f cleanerFunc) Cleanup(ctx context.Context) (int64, error) { return f(ctx) }

func TestWorkerBudgetCancellationNoOverlapOrImmediateRetry(t *testing.T) {
	if CleanupInterval != 15*time.Minute || CleanupTimeout != 2*time.Second {
		t.Fatal("worker defaults")
	}
	var logs bytes.Buffer
	var calls atomic.Int32
	entered := make(chan struct{})
	finished := make(chan struct{})
	ticks := make(chan time.Time, 1)
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWorker(cleanerFunc(func(c context.Context) (int64, error) {
		calls.Add(1)
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("unbounded cleanup")
		}
		close(entered)
		<-c.Done()
		return 0, user.ErrCanceled
	}), slog.New(slog.NewJSONHandler(&logs, nil)))
	go func() { defer close(finished); w.run(ctx, ticks) }()
	if calls.Load() != 0 {
		t.Fatal("cleanup before interval")
	}
	ticks <- time.Now()
	<-entered
	ticks <- time.Now()
	cancel()
	<-finished
	if calls.Load() != 1 || logs.Len() != 0 {
		t.Fatal("overlap or cancellation logging")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	entered = make(chan struct{}, 2)
	finished = make(chan struct{})
	ticks = make(chan time.Time)
	w = NewWorker(cleanerFunc(func(c context.Context) (int64, error) {
		entered <- struct{}{}
		return 0, errors.New("sentinel SQL password")
	}), slog.New(slog.NewJSONHandler(&logs, nil)))
	go func() { defer close(finished); w.run(ctx, ticks) }()
	ticks <- time.Now()
	<-entered
	// A rendezvous on the next tick proves the loop returned to waiting,
	// rather than starting an unprompted retry or spinning after failure.
	ticks <- time.Now()
	<-entered
	cancel()
	<-finished
	if strings.Contains(logs.String(), "sentinel") {
		t.Fatal("worker diagnostic disclosure")
	}
}

func TestWorkerDatabaseDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	observed := make(chan error, 1)
	w := NewWorker(cleanerFunc(func(c context.Context) (int64, error) { <-c.Done(); observed <- c.Err(); return 0, user.ErrTimeout }), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	w.timeout = time.Millisecond
	go func() { defer close(done); w.run(ctx, ticks) }()
	ticks <- time.Now()
	if <-observed != context.DeadlineExceeded {
		t.Fatal("deadline not inherited")
	}
	cancel()
	<-done
}

func TestCleanupWorkerDrainsBeforeResourcesClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ticks := make(chan time.Time)
	entered := make(chan struct{})
	stopped := make(chan struct{})
	w := NewWorker(cleanerFunc(func(ctx context.Context) (int64, error) {
		close(entered)
		<-ctx.Done()
		close(stopped)
		return 0, user.ErrCanceled
	}), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var draining atomic.Bool
	runner := app.Runner{Server: app.NewServer(listener.Addr().String(), http.NotFoundHandler()), Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), Draining: &draining, ShutdownTimeout: time.Second, Workers: []func(context.Context){func(ctx context.Context) { w.run(ctx, ticks) }}, CloseResource: func() error {
		select {
		case <-stopped:
		default:
			t.Error("resource closed before cleanup exit")
		}
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, listener) }()
	ticks <- time.Now()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err != nil || !draining.Load() {
			t.Fatal("worker graceful exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker extended shutdown budget")
	}
}
