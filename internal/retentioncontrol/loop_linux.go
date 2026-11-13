//go:build privateworker && linux

package retentioncontrol

import (
	"context"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"time"
)

// Sweep uses only the three already-approved Migration10 Controller entries.
// Every batch is its own2s transaction; no DB connection spans a sleep.
func Sweep(ctx context.Context, s scheduler.Store, cursor *string) (*string, error) {
	page, e := s.RetentionCandidates(ctx, cursor, 16)
	if e != nil {
		return nil, e
	}
	for _, h := range page.Items {
		if h.MonitorID == "" {
			continue
		} // Target-key cooling/retirement stays separate.
		var position scheduler.CleanupCursor
		for batches := 0; batches < 16; batches++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			o, e := s.CleanupMonitor(ctx, h, position, 8)
			if e != nil {
				return nil, e
			}
			position = o.CleanupCursor
			if position.Finished == nil || position.RunID == nil {
				break
			}
		}
	}
	return page.NextCursor, nil
}
func Run(ctx context.Context, s scheduler.Store) error {
	var cursor *string
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		next, e := Sweep(ctx, s, cursor)
		if e != nil {
			return e
		}
		cursor = next
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
