package monitoring

import (
	"embed"
	"encoding/csv"
	"net/netip"
	"strconv"
	"strings"
)

// registryFiles are reviewed build inputs, never fetched during execution.
//
//go:embed registry/*.csv registry/manifest.json
var registryFiles embed.FS

type addressPolicy struct{ allocated, denied []netip.Prefix }

var registeredPolicy = loadRegistry()

func loadRegistry() addressPolicy {
	var p addressPolicy
	for _, file := range []string{"ipv4-special", "ipv6-special", "ipv4-space", "ipv6-space"} {
		b, err := registryFiles.ReadFile("registry/" + file + ".csv")
		if err != nil {
			panic("pinned address registry missing")
		}
		rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
		if err != nil || len(rows) < 2 {
			panic("pinned address registry invalid")
		}
		for _, row := range rows[1:] {
			if len(row) < 6 {
				panic("pinned address registry invalid")
			}
			for _, block := range strings.Split(row[0], ",") {
				fields := strings.Fields(block)
				if len(fields) == 0 {
					panic("pinned address registry invalid")
				}
				block = fields[0]
				if file == "ipv4-space" {
					n, e := strconv.Atoi(strings.TrimSuffix(block, "/8"))
					if e != nil || n < 0 || n > 255 {
						panic("pinned address registry invalid")
					}
					block = netip.AddrFrom4([4]byte{byte(n)}).String() + "/8"
				}
				prefix, e := netip.ParsePrefix(block)
				if e != nil {
					panic("pinned address registry invalid")
				}
				prefix = prefix.Masked()
				if strings.HasSuffix(file, "special") || row[5] != "ALLOCATED" && row[5] != "LEGACY" {
					p.denied = append(p.denied, prefix)
				} else {
					p.allocated = append(p.allocated, prefix)
				}
			}
		}
	}
	// Multicast/reserved space is not exhaustively represented by special-use RR.
	for _, s := range []string{"224.0.0.0/4", "240.0.0.0/4", "192.88.99.0/24"} {
		p.denied = append(p.denied, netip.MustParsePrefix(s))
	}
	return p
}

func policyWithDeny(deny []netip.Prefix) (addressPolicy, error) {
	p := addressPolicy{allocated: registeredPolicy.allocated, denied: append([]netip.Prefix(nil), registeredPolicy.denied...)}
	for _, d := range deny {
		if !d.IsValid() || d.Addr().Is4In6() || d.Addr().Zone() != "" {
			return addressPolicy{}, fail(BlockedTarget)
		}
		p.denied = append(p.denied, d.Masked())
	}
	return p, nil
}
func (p addressPolicy) allowed(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" || ip.Is4In6() || !ip.IsGlobalUnicast() {
		return false
	}
	if ip.Is6() {
		if !netip.MustParsePrefix("2000::/3").Contains(ip) {
			return false
		}
		b := ip.As16()
		if (b[8] == 0 || b[8] == 2) && b[9] == 0 && b[10] == 0x5e && b[11] == 0xfe {
			return false
		}
	}
	for _, d := range p.denied {
		if d.Contains(ip) {
			return false
		}
	}
	for _, a := range p.allocated {
		if a.Contains(ip) {
			return true
		}
	}
	return false
}
