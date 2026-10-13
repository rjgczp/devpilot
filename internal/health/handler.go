package health

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

type Checker interface {
	Ping(context.Context) error
}

type Handler struct {
	Checker  Checker
	Timeout  time.Duration
	Draining *atomic.Bool
	Logger   *slog.Logger
}

func (h Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusOK, "alive")
	})
	mux.HandleFunc("GET /readyz", h.ready)
	return mux
}

func (h Handler) ready(w http.ResponseWriter, r *http.Request) {
	if h.Draining.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	if err := h.Checker.Ping(ctx); err != nil {
		h.Logger.Error("database readiness check failed", "reason", "unavailable")
		writeStatus(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	if h.Draining.Load() {
		writeStatus(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	writeStatus(w, http.StatusOK, "ready")
}

func writeStatus(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body + "\n"))
}
