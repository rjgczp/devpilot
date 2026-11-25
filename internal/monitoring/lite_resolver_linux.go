//go:build (schedulerfixture || privateprobe) && linux

package monitoring

import "net/netip"

// LiteResolverAllowed pins an explicit infrastructure endpoint. V1 deliberately
// accepts only ordinary public IPv4 shapes; tests route one inside isolation.
// It does not authorize HTTPS to the resolver or prove control of that server.
func LiteResolverAllowed(ip netip.Addr) bool {
	return ip.Is4() && PrivateTargetAllowed("resolver.controlled.example.com", ip, nil)
}
