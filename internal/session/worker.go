package session

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/rjgczp/devpilot/internal/user"
)

const CleanupInterval = 15 * time.Minute
const CleanupTimeout = 2 * time.Second

type Cleaner interface {
	Cleanup(context.Context) (int64, error)
}
type Worker struct {
	store   Cleaner
	logger  *slog.Logger
	timeout time.Duration
}

func NewWorker(store Cleaner, logger *slog.Logger) *Worker {
	return &Worker{store: store, logger: logger, timeout: CleanupTimeout}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()
	w.run(ctx, ticker.C)
}

// Work executes synchronously: ticks cannot create overlapping jobs or a queue.
func (w *Worker) run(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		if ctx.Err() != nil {
			return
		}
		dbctx, cancel := context.WithTimeout(ctx, w.timeout)
		_, err := w.store.Cleanup(dbctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			reason := "internal_error"
			switch {
			case errors.Is(err, user.ErrTimeout):
				reason = "db_timeout"
			case errors.Is(err, user.ErrUnavailable):
				reason = "db_unavailable"
			case errors.Is(err, user.ErrSchema):
				reason = "schema_missing"
			}
			w.logger.Error("session cleanup failed", "reason", reason)
		}
	}
}
