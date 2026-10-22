//go:build schedulerfixture && linux

package supervisorfixture

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

func testLineage() handoff.Lineage {
	var l handoff.Lineage
	_ = json.Unmarshal([]byte(`{"workspace_id":"01900000-0000-7000-8000-000000000001","project_id":"01900000-0000-7000-8000-000000000002","monitor_id":"01900000-0000-7000-8000-000000000003","run_id":"01900000-0000-7000-8000-000000000004","configuration_version":"1","activation_generation":"2","scheduler_epoch":"3","fencing_token":"4","namespace_incarnation":"01900000-0000-7000-8000-000000000005","supervisor_incarnation":"01900000-0000-7000-8000-000000000006"}`), &l)
	return l
}
func testConfig() Config {
	return Config{Lineage: testLineage(), Role: "run", Namespace: "net:[123]", RouteHash: strings.Repeat("a", 64), AddressHash: strings.Repeat("b", 64), Probe4: netip.MustParseAddr("198.19.64.2"), Probe6: netip.MustParseAddr("fd64:64:1::2"), Host: "a.fixture.example.com", HardDeadline: time.Now().Add(30 * time.Second)}
}

func TestFixtureNetworkConfigAndRules(t *testing.T) {
	c := testConfig()
	if !validConfig(c, time.Now()) {
		t.Fatal("valid fixture config rejected")
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.HardDeadline = time.Now().Add(time.Hour) }, func(c *Config) { c.Host = "production.example.com" }, func(c *Config) { c.Lineage.RunID = "" }, func(c *Config) { c.Probe4 = netip.MustParseAddr("127.0.0.1") }, func(c *Config) { c.Role = "worker" }} {
		x := c
		mutate(&x)
		if validConfig(x, time.Now()) {
			t.Fatal("invalid configuration accepted")
		}
	}
	for _, role := range []string{"run", "gateway"} {
		c.Role = role
		s := policy(c)
		if strings.Count(s, "policy drop") != 3 || strings.Contains(s, "established") || strings.Contains(s, "udp") || !strings.Contains(s, "flags timeout") {
			t.Fatal("policy widened")
		}
	}
	for _, s := range []string{"169.254.169.254", "10.0.0.9", "11.0.0.9", "192.0.2.53", "::1", "fd64:53::53", "64:ff9b::5db8:d822"} {
		if fixtureTarget(netip.MustParseAddr(s)) {
			t.Fatal("unregistered target accepted")
		}
	}
}
func TestFixtureNetworkManagementDecode(t *testing.T) {
	var c Config
	for _, s := range []string{`{"role":"run","role":"gateway"}`, `{"unknown":true}`, `{} {}`, strings.Repeat(" ", 4097)} {
		if Decode([]byte(s), &c) == nil {
			t.Fatal("ambiguous or oversized request accepted")
		}
	}
}
func TestFixtureNetworkDeniedAttemptIsNotRenewable(t *testing.T) {
	c := testConfig()
	p := &Policy{c: c, hardMono: time.Now().Add(30 * time.Second)}
	x := Install{Request: targetpermit.Request{Lineage: c.Lineage, Host: c.Host, Scheme: "https", IP: netip.MustParseAddr("93.184.216.34"), Port: 443, Deadline: time.Now().Add(time.Second)}}
	// Invalid proof fails before any kernel command and consumes the operation.
	if _, e := p.Install(context.Background(), x); e == nil {
		t.Fatal("uncommitted target proof accepted")
	}
	x.PermitID = "01900000-0000-7000-8000-000000000007"
	x.CommittedAt = time.Now()
	x.PermitDeadline = x.Request.Deadline
	if _, e := p.Install(context.Background(), x); e == nil {
		t.Fatal("denied attempt renewed")
	}
}
func TestFixtureNetworkAuditKeepsSecurityStructure(t *testing.T) {
	a := map[string]any{"counter": map[string]any{"packets": 1, "bytes": 2}, "policy": "drop", "set": map[string]any{"name": "target4", "elem": []any{"short-lived"}}}
	b := map[string]any{"counter": map[string]any{"packets": 9, "bytes": 20}, "policy": "drop", "set": map[string]any{"name": "target4", "elem": []any{}}}
	if digest(canonical(stable(a))) != digest(canonical(stable(b))) {
		t.Fatal("runtime counters changed audit identity")
	}
	b["policy"] = "accept"
	if digest(canonical(stable(a))) == digest(canonical(stable(b))) {
		t.Fatal("audit ignored fail-open policy")
	}
}

func TestFixtureStopCannotRevokeAnotherNamespace(t *testing.T) {
	// A network-none/all-capabilities-dropped Linux test container is used by the
	// runner. This rejection performs no mutation and is not physical-stop proof.
	c := testConfig()
	if _, e := Revoke(context.Background(), c); e == nil {
		t.Fatal("foreign namespace revoked")
	}
}
