//go:build linux

package storagecontrol

import (
	"context"
	"io"
	"time"
)

// Run collects after the DB challenge, outside a transaction, and publishes
// once. A failure never replaces last-good with zero or retries an unknown ACK.
// heartbeat is inherited one-way IPC to an independent monotonic watchdog.
func Run(ctx context.Context, p Publisher, s *Sampler, heartbeat io.Writer) error {
	if p == nil || s == nil || heartbeat == nil {
		return ErrReport
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	previous := time.Now()
	deny := func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.Unavailable(c)
	}
	defer deny()
	for {
		now := time.Now()
		mono := now.Sub(previous)
		wall := time.Duration(now.UnixNano() - previous.UnixNano())
		delta := wall - mono
		if delta < 0 {
			delta = -delta
		}
		if delta > 500*time.Millisecond {
			return ErrReport
		}
		previous = now
		ch, e := p.Begin(ctx)
		if e != nil {
			return e
		}
		v, e := s.Collect()
		if e != nil {
			return e
		}
		if time.Since(now) >= 2*time.Second {
			return ErrReport
		}
		if e = p.Publish(ctx, ch, map[string]any{"sequence": ch.Sequence, "filesystems": v}); e != nil {
			return e
		}
		if time.Since(now) >= 2*time.Second {
			return ErrReport
		}
		beat, err := pulse()
		if err != nil {
			return err
		}
		if _, e = heartbeat.Write(beat); e != nil {
			return ErrReport
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
