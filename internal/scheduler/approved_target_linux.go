//go:build (schedulerfixture || privateworker) && linux

package scheduler

import (
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"net/netip"
	"net/url"
	"time"
)

// TargetApproval is operator-owned offline configuration, never Monitor input.
// Owner consent is independently required by the original PG control functions.
// Each approval covers one exact URL and Monitor configuration, not a hostname
// wildcard, redirect, different tenant, changed DNS graph or later edit.
type TargetApproval struct {
	WorkspaceID          string       `json:"workspace_id"`
	ProjectID            string       `json:"project_id"`
	MonitorID            string       `json:"monitor_id"`
	ConfigurationVersion string       `json:"configuration_version"`
	URL                  string       `json:"url"`
	Addresses            []netip.Addr `json:"addresses"`
	DNSDigest            string       `json:"dns_digest"`
	ApprovedAt           time.Time    `json:"approved_at"`
	ExpiresAt            time.Time    `json:"expires_at"`
}

func (a TargetApproval) matches(h Hint) bool {
	return a.WorkspaceID == h.WorkspaceID && a.ProjectID == h.ProjectID && a.MonitorID == h.MonitorID && a.ConfigurationVersion == h.ConfigurationVersion
}
func (a TargetApproval) fresh(now time.Time) bool {
	return !a.ApprovedAt.After(now) && a.ExpiresAt.After(now) && a.ExpiresAt.After(a.ApprovedAt) && a.ExpiresAt.Sub(a.ApprovedAt) <= 24*time.Hour
}
func validDNSDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, b := range []byte(s) {
		if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
			return false
		}
	}
	return true
}

func NewApprovedDirectory(keys map[int64][]byte, approvals []TargetApproval) (TargetDirectory, error) {
	if len(approvals) == 0 || len(approvals) > 100 {
		return TargetDirectory{}, ErrCandidate
	}
	var targets []FixtureTarget
	for _, a := range approvals {
		u, e := url.Parse(a.URL)
		if e != nil || u.Scheme != "https" || !monitor.ValidStoredConfiguration("approved", a.URL, "GET", 60, 10, 200) || !validDNSDigest(a.DNSDigest) || !a.fresh(time.Now()) {
			return TargetDirectory{}, ErrCandidate
		}
		for _, ip := range a.Addresses {
			if !monitoring.PrivateTargetAllowed(u.Hostname(), ip, nil) {
				return TargetDirectory{}, ErrCandidate
			}
		}
		targets = append(targets, FixtureTarget{URL: a.URL, Addresses: a.Addresses})
	}
	d, e := NewTargetDirectory(keys, targets)
	if e != nil {
		return TargetDirectory{}, e
	}
	d.approved = true
	for _, a := range approvals {
		if len(a.Addresses) == 0 {
			return TargetDirectory{}, ErrCandidate
		}
		a.Addresses = append([]netip.Addr(nil), a.Addresses...)
		h := Hint{WorkspaceID: a.WorkspaceID, ProjectID: a.ProjectID, MonitorID: a.MonitorID, ConfigurationVersion: a.ConfigurationVersion}
		if _, e = d.Bind(h, a.URL, a.Addresses[0]); e != nil {
			return TargetDirectory{}, e
		}
		for _, old := range d.approvals {
			if old.matches(h) {
				return TargetDirectory{}, ErrCandidate
			}
		}
		d.approvals = append(d.approvals, a)
	}
	return d, nil
}
func (d TargetDirectory) ApprovedMode() bool { return d.approved }
func (d TargetDirectory) AllowsHint(h Hint) bool {
	if !d.approved {
		return true
	}
	for _, a := range d.approvals {
		if a.matches(h) && a.fresh(time.Now()) && a.ExpiresAt.After(time.Now().Add(45*time.Second)) {
			return true
		}
	}
	return false
}
func (d TargetDirectory) AllowsRequest(h Hint, raw string, r targetpermit.Request) bool {
	if !d.approved {
		return true
	}
	for _, a := range d.approvals {
		if !a.matches(h) || a.URL != raw || !a.fresh(time.Now()) || r.DNSDigest != a.DNSDigest || r.Deadline.After(a.ExpiresAt) {
			continue
		}
		for _, ip := range a.Addresses {
			if ip == r.IP {
				return true
			}
		}
	}
	return false
}
