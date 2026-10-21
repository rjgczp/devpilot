//go:build schedulerfixture && linux

package monitoring

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

func TestSchedulerPermitBridgeBindsFrozenInput(t *testing.T) {
	const id = "10000000-0000-0000-0000-000000000001"
	input := SchedulerInput{snapshot: testSnapshot(1), lineage: handoff.Lineage{WorkspaceID: id, ProjectID: id, MonitorID: id, RunID: id, ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "3", FencingToken: "4", NamespaceIncarnation: id, SupervisorIncarnation: id}, deadline: time.Now().UTC().Add(time.Second), permitAttempt: &atomic.Bool{}}
	input.snapshot.target = "https://a.fixture.example.com/URL_PATH_SECRET"
	rr, rw, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	ar, aw, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer rr.Close()
	defer rw.Close()
	defer ar.Close()
	defer aw.Close()
	c := targetCandidate{scope: input.snapshot.scope, version: input.snapshot.version, scheme: "https", host: "a.fixture.example.com", endpoint: netip.MustParseAddrPort("93.184.216.34:443"), deadline: time.Now().Add(10 * time.Second)}
	done := make(chan error, 1)
	go func() {
		done <- (&targetpermit.Server{}).Serve(context.Background(), rr, aw, func(_ context.Context, r targetpermit.Request) (targetpermit.Confirmation, error) {
			if r.Lineage != input.lineage || r.IP != c.endpoint.Addr() || r.Host != c.host || r.Port != 443 || !r.Deadline.Equal(input.deadline) {
				t.Error("frozen binding changed")
			}
			if strings.Contains(fmt.Sprintf("%+v", r), "fixture.example.com") {
				t.Error("ordinary diagnostic exposed target")
			}
			return targetpermit.Confirmation{Request: r, PermitID: id, Deadline: r.Deadline}, nil
		})
	}()
	client := targetpermit.NewClient(rw, ar)
	if input.targetHook(client)(context.Background(), c) != nil {
		t.Fatal("offline bound permit")
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	copy := input
	if copy.targetHook(client)(context.Background(), c) == nil {
		t.Fatal("copy restored same run permission")
	}
}
