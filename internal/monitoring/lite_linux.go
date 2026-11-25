//go:build privateprobe && linux

package monitoring

import (
	"context"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"net/netip"
	"net/url"
	"os"
	"time"
)

func (s SchedulerInput) ExecuteLite(ctx context.Context, client *targetpermit.Client, supervisor *os.File, deny []netip.Prefix, expires time.Time, resolver netip.Addr) (*Result, error) {
	if !LiteResolverAllowed(resolver) || len(deny) == 0 || !expires.After(time.Now()) || expires.After(time.Now().Add(24*time.Hour)) {
		return nil, ErrTargetPermit
	}
	return s.executeResolved(ctx, client, supervisor, deny, expires, resolver, true)
}

func ReviewLiteDNS(ctx context.Context, raw string, deny []netip.Prefix, resolver netip.Addr) (DNSReview, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || !LiteResolverAllowed(resolver) || !monitor.ValidStoredConfiguration("review", raw, "GET", 60, 10, 200) {
		return DNSReview{}, ErrInvalidConfiguration
	}
	p, e := policyWithDeny(deny)
	if e != nil {
		return DNSReview{}, e
	}
	return (dnsResolver{address: resolver, policy: p}).review(ctx, u.Hostname())
}
