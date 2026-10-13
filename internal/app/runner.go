package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Runner owns the HTTP server and any resources tied to its lifetime.
type Runner struct {
	Server          *http.Server
	Logger          *slog.Logger
	Draining        *atomic.Bool
	ShutdownTimeout time.Duration
	CloseResource   func() error
	Workers         []func(context.Context)
}

func (r Runner) Run(signalContext context.Context, listener net.Listener) error {
	workContext, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	var workers sync.WaitGroup
	for _, worker := range r.Workers {
		workers.Add(1)
		go func(run func(context.Context)) {
			defer workers.Done()
			run(workContext)
		}(worker)
	}

	serveResult := make(chan error, 1)
	go func() { serveResult <- r.Server.Serve(listener) }()
	r.Logger.Info("server started", "address", listener.Addr().String())

	var serveFailure error
	select {
	case <-signalContext.Done():
	case err := <-serveResult:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveFailure = err
		}
	}

	r.Draining.Store(true)
	cancelWork()
	deadline := time.Now().Add(r.ShutdownTimeout)
	shutdownContext, cancelShutdown := context.WithDeadline(context.Background(), deadline)
	defer cancelShutdown()

	shutdownErr := r.Server.Shutdown(shutdownContext)
	if shutdownErr != nil {
		r.Logger.Error("server shutdown failed", "reason", shutdownReason(shutdownErr))
		if err := r.Server.Close(); err != nil {
			r.Logger.Error("server force close failed", "reason", "http_close_error")
		}
	}

	workersDone := make(chan struct{})
	go func() {
		workers.Wait()
		close(workersDone)
	}()
	var workerErr error
	select {
	case <-workersDone:
	case <-shutdownContext.Done():
		workerErr = shutdownContext.Err()
	}

	resourceResult := make(chan error, 1)
	go func() {
		if r.CloseResource != nil {
			resourceResult <- r.CloseResource()
			return
		}
		resourceResult <- nil
	}()
	var resourceErr error
	select {
	case err := <-resourceResult:
		if err != nil {
			resourceErr = err
			r.Logger.Error("resource cleanup failed", "reason", "resource_close_error")
		}
	case <-shutdownContext.Done():
		resourceErr = shutdownContext.Err()
	}
	if errors.Is(workerErr, context.DeadlineExceeded) || errors.Is(resourceErr, context.DeadlineExceeded) {
		r.Logger.Error("resource cleanup failed", "reason", "deadline_exceeded")
	}
	deadlineErr := shutdownContext.Err()
	if deadlineErr != nil && shutdownErr == nil && workerErr == nil && resourceErr == nil {
		r.Logger.Error("service shutdown failed", "reason", "deadline_exceeded")
	}
	if serveFailure != nil {
		r.Logger.Error("server stopped unexpectedly", "reason", "serve_error")
		return errors.New("HTTP serving failed")
	}
	if shutdownErr != nil || workerErr != nil || resourceErr != nil || deadlineErr != nil {
		return errors.New("service shutdown failed")
	}
	r.Logger.Info("server stopped")
	return nil
}

func shutdownReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "http_shutdown_error"
}
