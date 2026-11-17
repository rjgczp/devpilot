//go:build (schedulerfixture || privateworker) && linux

package scheduler

import (
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestApprovedDirectoryScopeDNSAndExpiry(t *testing.T) {
	h := Hint{WorkspaceID: "01900000-0000-7000-8000-000000000001", ProjectID: "01900000-0000-7000-8000-000000000002", MonitorID: "01900000-0000-7000-8000-000000000003", ConfigurationVersion: "1"}
	now := time.Now()
	a := TargetApproval{WorkspaceID: h.WorkspaceID, ProjectID: h.ProjectID, MonitorID: h.MonitorID, ConfigurationVersion: h.ConfigurationVersion, URL: "https://monitor.owned-test.example.com/check", Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, DNSDigest: strings.Repeat("a", 64), ApprovedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}
	d, e := NewApprovedDirectory(map[int64][]byte{1: []byte(strings.Repeat("k", 32))}, []TargetApproval{a})
	if e != nil {
		t.Fatal(e)
	}
	r := targetpermit.Request{DNSDigest: a.DNSDigest, IP: a.Addresses[0], Deadline: now.Add(10 * time.Second)}
	if !d.AllowsHint(h) || !d.AllowsRequest(h, a.URL, r) {
		t.Fatal("approved target denied")
	}
	for _, change := range []func(*Hint, *string, *targetpermit.Request){
		func(h *Hint, _ *string, _ *targetpermit.Request) {
			h.ProjectID = "01900000-0000-7000-8000-000000000009"
		},
		func(h *Hint, _ *string, _ *targetpermit.Request) { h.ConfigurationVersion = "2" },
		func(_ *Hint, u *string, _ *targetpermit.Request) { *u += "/other" },
		func(_ *Hint, _ *string, r *targetpermit.Request) { r.DNSDigest = strings.Repeat("b", 64) },
		func(_ *Hint, _ *string, r *targetpermit.Request) { r.IP = netip.MustParseAddr("93.184.216.35") },
		func(_ *Hint, _ *string, r *targetpermit.Request) { r.Deadline = a.ExpiresAt.Add(time.Nanosecond) },
	} {
		hh, u, rr := h, a.URL, r
		change(&hh, &u, &rr)
		if d.AllowsRequest(hh, u, rr) {
			t.Fatal("unapproved request accepted")
		}
	}
	// Caller mutation cannot extend immutable process approval.
	a.Addresses[0] = netip.MustParseAddr("93.184.216.35")
	if !d.AllowsRequest(h, a.URL, r) {
		t.Fatal("mutable address alias")
	}
	d.approvals[0].ExpiresAt = time.Now().Add(-time.Second)
	if d.AllowsHint(h) || d.AllowsRequest(h, a.URL, r) {
		t.Fatal("expired approval")
	}
}
