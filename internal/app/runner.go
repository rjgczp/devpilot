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
	CloseResource   func()
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
	resourceDone := make(chan struct{})
	go func() {
		if r.CloseResource != nil {
			r.CloseResource()
		}
		close(resourceDone)
	}()

	var cleanupErr error
	pendingWorkers, pendingResource := workersDone, resourceDone
	for pendingWorkers != nil || pendingResource != nil {
		select {
		case <-pendingWorkers:
			pendingWorkers = nil
		case <-pendingResource:
			pendingResource = nil
		case <-shutdownContext.Done():
			cleanupErr = shutdownContext.Err()
			r.Logger.Error("resource cleanup failed", "reason", "deadline_exceeded")
			pendingWorkers = nil
			pendingResource = nil
		}
	}
	if serveFailure != nil {
		r.Logger.Error("server stopped unexpectedly", "reason", "serve_error")
		return errors.New("HTTP serving failed")
	}
	if shutdownErr != nil || cleanupErr != nil {
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
