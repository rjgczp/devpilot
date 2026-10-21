//go:build schedulerfixture && linux

package monitoring

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

// SchedulerInput keeps the original kernel's private Snapshot; there is no
// network execution method. A future independently reviewed target-permit
// bridge must reuse the private engine, never replace its DNS/TLS/HTTP client.
type SchedulerInput struct {
	snapshot Snapshot
	lineage  handoff.Lineage
}

func (SchedulerInput) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<sealed scheduler input>")
}
func (s SchedulerInput) Lineage() handoff.Lineage { return s.lineage }
func (s SchedulerInput) Valid() bool              { return s.snapshot.valid() && s.lineage.Valid() }
func ReceiveSchedulerInput(receiver *handoff.Receiver, input *os.File) (SchedulerInput, error) {
	e, err := receiver.Receive(input)
	if err != nil {
		return SchedulerInput{}, handoff.ErrInput
	}
	v, err := strconv.ParseInt(e.ConfigurationVersion, 10, 64)
	if err != nil {
		return SchedulerInput{}, handoff.ErrInput
	}
	s := Snapshot{scope: Scope{e.WorkspaceID, e.ProjectID, e.MonitorID}, version: v, name: "monitor", target: string(e.Target), method: "GET", interval: int64(e.IntervalSeconds), timeout: int64(e.TimeoutSeconds), expected: int64(e.ExpectedStatus)}
	if !s.valid() {
		return SchedulerInput{}, handoff.ErrInput
	}
	return SchedulerInput{snapshot: s, lineage: e.Lineage}, nil
}
