//go:build schedulerfixture && linux

package supervisorfixture

import (
	"encoding/json"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"net/netip"
	"strings"
	"time"
)

func testLineage() handoff.Lineage {
	var l handoff.Lineage
	_ = json.Unmarshal([]byte(`{"workspace_id":"01900000-0000-7000-8000-000000000001","project_id":"01900000-0000-7000-8000-000000000002","monitor_id":"01900000-0000-7000-8000-000000000003","run_id":"01900000-0000-7000-8000-000000000004","configuration_version":"1","activation_generation":"2","scheduler_epoch":"3","fencing_token":"4","namespace_incarnation":"01900000-0000-7000-8000-000000000005","supervisor_incarnation":"01900000-0000-7000-8000-000000000006"}`), &l)
	return l
}
func testConfig() Config {
	return Config{Lineage: testLineage(), Role: "run", Namespace: "net:[123]", RouteHash: strings.Repeat("a", 64), AddressHash: strings.Repeat("b", 64), Probe4: netip.MustParseAddr("198.19.64.2"), Probe6: netip.MustParseAddr("fd64:64:1::2"), Host: "a.fixture.example.com", HardDeadline: time.Now().Add(30 * time.Second)}
}
