//go:build schedulerfixture && linux

package supervisorfixture

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

// NetworkStop is an independently observed network revocation, not a process
// exit receipt and never permission to release a database reservation.
type NetworkStop struct {
	Lineage        handoff.Lineage `json:"lineage"`
	Namespace      string          `json:"namespace"`
	Role           string          `json:"role"`
	RulesHash      string          `json:"rules_hash"`
	LinkRemoved    bool            `json:"link_removed"`
	ConntrackEmpty bool            `json:"conntrack_empty"`
	StoppedAt      time.Time       `json:"stopped_at"`
}

// Revoke is also available after the original policy control channel is lost.
// Its authority is the private bootstrap ledger and actual owned namespace;
// neither a Worker assertion nor a lease expiry can invoke this operation.
func Revoke(ctx context.Context, c Config) (NetworkStop, error) {
	denied := NetworkStop{}
	if !c.Lineage.Valid() || (c.Role != "run" && c.Role != "gateway") || os.Geteuid() != 0 {
		return denied, ErrDenied
	}
	ns, e := os.Readlink("/proc/self/ns/net")
	if e != nil || ns != c.Namespace {
		return denied, ErrDenied
	}
	status, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return denied, ErrDenied
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(status), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[k] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"CapEff", "CapPrm", "CapBnd"} {
		if fields[k] != "0000000000001000" {
			return denied, ErrDenied
		}
	}
	if fields["NoNewPrivs"] != "1" || fields["CapInh"] != "0000000000000000" || fields["CapAmb"] != "0000000000000000" {
		return denied, ErrDenied
	}
	want := canonical(map[string]string{"namespace": ns, "lineage_hash": digest(canonical(c.Lineage))})
	ledger, e := os.ReadFile("/tmp/devpilot-network64.boot")
	if e != nil || string(ledger) != string(want) {
		return denied, ErrDenied
	}
	// A durable tombstone precedes every destructive step. Partial failure can
	// only leave this incarnation unusable; no restart may bootstrap it again.
	path := "/tmp/devpilot-network64.stopped"
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(e) {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != string(want) {
			return denied, ErrDenied
		}
	} else {
		if e != nil {
			return denied, ErrDenied
		}
		_, we := f.Write(want)
		se := f.Sync()
		ce := f.Close()
		if we != nil || se != nil || ce != nil {
			return denied, ErrDenied
		}
	}
	script := "flush ruleset\ntable inet dp_stopped {\n"
	for _, hook := range []string{"input", "output", "forward"} {
		script += "chain " + hook + " { type filter hook " + hook + " priority -300; policy drop; counter drop; }\n"
	}
	script += "}\n"
	if _, e = command(ctx, script, "/usr/sbin/nft", "-f", "-"); e != nil {
		return denied, ErrDenied
	}
	// This removes the actual point-to-point wire, including established flows.
	// No conntrack mark, source port or subsequently removed rule can restore it.
	v, e := kernelJSON(ctx, "/usr/sbin/ip", "-j", "-d", "link", "show")
	if e != nil {
		return denied, ErrDenied
	}
	rows, ok := v.([]any)
	if !ok {
		return denied, ErrDenied
	}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return denied, ErrDenied
		}
		if m["ifname"] == "eth0" {
			info, _ := m["linkinfo"].(map[string]any)
			if info["info_kind"] != "veth" {
				return denied, ErrDenied
			}
			if _, e = command(ctx, "", "/usr/sbin/ip", "link", "delete", "dev", "eth0"); e != nil {
				return denied, ErrDenied
			}
		}
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		if _, e = command(ctx, "", "/usr/sbin/conntrack", "-F", "-f", family); e != nil {
			return denied, ErrDenied
		}
		b, err := command(ctx, "", "/usr/sbin/conntrack", "-L", "-f", family)
		if err != nil || len(strings.TrimSpace(string(b))) != 0 {
			return denied, ErrDenied
		}
	}
	v, e = kernelJSON(ctx, "/usr/sbin/ip", "-j", "link", "show")
	if e != nil {
		return denied, ErrDenied
	}
	rows, ok = v.([]any)
	if !ok {
		return denied, ErrDenied
	}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok || m["ifname"] == "eth0" {
			return denied, ErrDenied
		}
	}
	rules, e := kernelJSON(ctx, "/usr/sbin/nft", "-j", "list", "ruleset")
	if e != nil {
		return denied, ErrDenied
	}
	object, ok := rules.(map[string]any)
	if !ok {
		return denied, ErrDenied
	}
	items, ok := object["nftables"].([]any)
	if !ok {
		return denied, ErrDenied
	}
	chains := 0
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return denied, ErrDenied
		}
		for kind, value := range m {
			if kind == "metainfo" {
				continue
			}
			z, ok := value.(map[string]any)
			if !ok {
				return denied, ErrDenied
			}
			if kind == "table" {
				if z["name"] != "dp_stopped" || z["family"] != "inet" {
					return denied, ErrDenied
				}
			} else {
				if z["table"] != "dp_stopped" || z["family"] != "inet" {
					return denied, ErrDenied
				}
				if kind == "chain" {
					if z["policy"] != "drop" {
						return denied, ErrDenied
					}
					chains++
				} else if kind != "rule" {
					return denied, ErrDenied
				}
			}
		}
	}
	if chains != 3 {
		return denied, ErrDenied
	}
	return NetworkStop{Lineage: c.Lineage, Namespace: ns, Role: c.Role, RulesHash: digest(canonical(stable(rules))), LinkRemoved: true, ConntrackEmpty: true, StoppedAt: time.Now().UTC()}, nil
}
