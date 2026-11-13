//go:build (schedulerfixture || privateprobe) && linux

package monitoring

import (
	"github.com/rjgczp/devpilot/internal/monitor"
	"net/netip"
	"net/url"
)

// PrivateTargetAllowed only subtracts from the original pinned address policy.
// Host/control exclusions are supplied by the trusted allocator, never Probe.
func PrivateTargetAllowed(host string, ip netip.Addr, deny []netip.Prefix) bool {
	u, err := url.Parse("https://" + host + "/")
	if err != nil || u.Host != host || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return false
	}
	p, e := policyWithDeny(deny)
	return e == nil && p.allowed(ip) && monitor.ValidStoredConfiguration("private", "https://"+host+"/", "GET", 60, 30, 200)
}
