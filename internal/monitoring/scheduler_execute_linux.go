//go:build schedulerfixture && linux

package monitoring

import (
	"context"
	"net/netip"

	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

// ExecuteFixture runs the original engine once, only in the separately sealed
// no-upstream fixture. It has no resolver override, custom roots or HTTP client.
// Copies share the burn bit, including every refusal. Results remain in memory;
// physical settlement does not complete the attempt in the database.
func (s SchedulerInput) ExecuteFixture(ctx context.Context, client *targetpermit.Client) (*Result, error) {
	if !s.Valid() || s.executeAttempt == nil || !s.executeAttempt.CompareAndSwap(false, true) || client == nil {
		return nil, ErrTargetPermit
	}
	ctx, cancel := context.WithDeadline(ctx, s.deadline)
	defer cancel()
	e, err := newEngine(netip.MustParseAddr("192.0.2.53"), nil)
	if err != nil {
		return nil, err
	}
	e.targetPermit = s.targetHook(client)
	return e.execute(ctx, s.snapshot)
}
