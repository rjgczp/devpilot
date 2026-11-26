//go:build privateprobe && linux

package monitoring

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestLiteResolverIsExplicitAndNeverDocumentationOrMetadata(t *testing.T) {
	for _, v := range []string{"", "192.0.2.53", "127.0.0.1", "169.254.169.254", "10.0.0.1", "::1", "2606:4700:4700::1111"} {
		ip, _ := netip.ParseAddr(v)
		if LiteResolverAllowed(ip) {
			t.Fatalf("accepted infrastructure %q", v)
		}
	}
	if !LiteResolverAllowed(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("pinned IPv4 rejected")
	}
	var s SchedulerInput
	if _, e := s.ExecuteLite(context.Background(), nil, nil, nil, time.Now().Add(time.Minute), netip.MustParseAddr("192.0.2.53")); e != ErrTargetPermit {
		t.Fatal("invalid profile did not close before network")
	}
}
