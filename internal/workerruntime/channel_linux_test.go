//go:build privateworker && linux

package workerruntime

import (
	"context"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"os"
	"testing"
	"time"
)

func TestSupervisorChannelLostOrMismatchedNeverReconnects(t *testing.T) {
	for _, reply := range []string{"", `{"event":"ok","line":{"run_id":"other"}}` + "\n"} {
		rr, rw, _ := os.Pipe()
		qr, qw, _ := os.Pipe()
		sup, e := newPipeSupervisor(rr, qw)
		if e != nil {
			t.Fatal(e)
		}
		go func() {
			b := make([]byte, 4096)
			qr.Read(b)
			if reply != "" {
				rw.Write([]byte(reply))
			}
			rw.Close()
			qr.Close()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if _, e = sup.Call(ctx, Call{Op: "next", Line: &handoff.Lineage{RunID: "expected"}}); e == nil {
			t.Fatal("lost/mismatched peer accepted")
		}
		if _, e = sup.Call(ctx, Call{Op: "next"}); e == nil {
			t.Fatal("channel replayed")
		}
		cancel()
		sup.Close()
	}
}
