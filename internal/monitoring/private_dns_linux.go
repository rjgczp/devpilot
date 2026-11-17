//go:build privateprobe && linux

package monitoring

import (
	"context"
	"github.com/rjgczp/devpilot/internal/monitor"
	"net/netip"
	"net/url"
)

// ReviewPrivateDNS reuses the complete engine resolver and pinned safety policy.
// It performs DNS only on the same fixed independent resolver wire as Probe.
// Operators must run it in their controlled no-upstream review namespace. It
// grants no Permit, SQL identity, routing, policy or target-GET capability.
func ReviewPrivateDNS(ctx context.Context, raw string) (DNSReview, error) {
	return ReviewPrivateDNSDeny(ctx, raw, nil)
}
func ReviewPrivateDNSDeny(ctx context.Context, raw string, deny []netip.Prefix) (DNSReview, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !monitor.ValidStoredConfiguration("review", raw, "GET", 60, 10, 200) {
		return DNSReview{}, ErrInvalidConfiguration
	}
	p, err := policyWithDeny(deny)
	if err != nil {
		return DNSReview{}, err
	}
	return (dnsResolver{address: netip.MustParseAddr("192.0.2.53"), policy: p}).review(ctx, u.Hostname())
}
