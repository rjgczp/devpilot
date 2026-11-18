package monitoring

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// DNSBinding hashes the complete validated A/AAAA/CNAME record set, including
// authority/additional sections and both families. Ordering, duplicate RRs and
// decreasing cache TTLs do not change identity. No resolution or permission is
// performed by this helper. DNS validation still happens afresh for every Run.
func DNSBinding(records []string) string {
	x := slices.Clone(records)
	slices.Sort(x)
	x = slices.Compact(x)
	h := sha256.Sum256([]byte("devpilot-dns-v1\n" + strings.Join(x, "\n")))
	return hex.EncodeToString(h[:])
}

// DNSReview is public operator metadata, not a target authorization or Result.
// It contains neither URL path nor credentials. Every execution resolves again.
type DNSReview struct {
	Host       string       `json:"host"`
	Addresses  []netip.Addr `json:"addresses"`
	Records    []string     `json:"records"`
	ObservedAt time.Time    `json:"observed_at"`
}
