//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"testing"
)

func TestRecoveryRejectsInvalidInputBeforeDatabase(t *testing.T) {
	l, _ := resultExample()
	if _, e := (Store{}).RecoverStarted(context.Background(), handoff.Lineage{}, "worker_lost"); e != ErrCandidate {
		t.Fatal(e)
	}
	for _, reason := range []string{"", "raw_error_COOKIE_SENTINEL", "network_failed", "completed"} {
		if _, e := (Store{}).RecoverStarted(context.Background(), l, reason); e != ErrCandidate {
			t.Fatal(e)
		}
	}
}
