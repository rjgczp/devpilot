//go:build schedulerfixture && linux

// Package supervisorfixture manages only the reviewed, no-upstream Linux test
// topology. It is excluded from ordinary builds and is not a production service.
package supervisorfixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

var ErrDenied = errors.New("fixture_network_unavailable")

var (
	errConsumed    = errors.New("fixture_network_consumed")
	errBinding     = errors.New("fixture_network_binding")
	errTopology    = errors.New("fixture_network_topology")
	errRules       = errors.New("fixture_network_rules")
	errExpiry      = errors.New("fixture_network_expiry")
	errKernel      = errors.New("fixture_network_kernel")
	errFutureProof = errors.New("fixture_network_future_commit_proof")
)

// FailureCode exposes only fixed management-stage diagnostics, never tool
// stderr, configuration, addresses, tenant IDs or a target error.
func FailureCode(e error) string {
	for _, known := range []error{errConsumed, errBinding, errTopology, errRules, errExpiry, errKernel, errFutureProof} {
		if errors.Is(e, known) {
			return known.Error()
		}
	}
	return ErrDenied.Error()
}

const kernelCommandBudget = 2 * time.Second

// Config is sent by the trusted outer fixture allocator over stdin, never by a
// Probe. The allocator independently creates Docker network-none namespaces,
// attaches only fresh point-to-point veth peers to its owned namespaces, and
// attests that there is no host interface, bridge, or published port.
// Route hashes bind the actual allocated namespace, not an environment flag.
type Config struct {
	Lineage      handoff.Lineage `json:"lineage"`
	Role         string          `json:"role"`
	Namespace    string          `json:"namespace"`
	RouteHash    string          `json:"route_hash"`
	AddressHash  string          `json:"address_hash"`
	Probe4       netip.Addr      `json:"probe4"`
	Probe6       netip.Addr      `json:"probe6"`
	Host         string          `json:"host"`
	HardDeadline time.Time       `json:"hard_deadline"`
}

type Install struct {
	Request        targetpermit.Request `json:"request"`
	PermitID       string               `json:"permit_id"`
	CommittedAt    time.Time            `json:"committed_at"`
	PermitDeadline time.Time            `json:"permit_deadline"`
}

type Ready struct {
	Namespace    string    `json:"namespace"`
	Role         string    `json:"role"`
	LineageHash  string    `json:"lineage_hash"`
	RouteHash    string    `json:"route_hash"`
	AddressHash  string    `json:"address_hash"`
	RulesHash    string    `json:"rules_hash"`
	HardDeadline time.Time `json:"hard_deadline"`
}

func (Config) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<private fixture network configuration>")
}
func (Install) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<private fixture network installation>")
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func canonical(v any) []byte { b, _ := json.Marshal(v); return b }

func command(ctx context.Context, input string, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, kernelCommandBudget)
	defer cancel()
	c := exec.CommandContext(ctx, path, args...)
	c.Env = []string{"LANG=C", "TZ=UTC", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	c.Stdin = strings.NewReader(input)
	// This namespace contains only bounded fixture objects. Raw tool errors never
	// escape the management boundary or become run results/log messages.
	b, e := c.Output()
	if e != nil || len(b) > 65536 || ctx.Err() != nil {
		return nil, ErrDenied
	}
	return b, nil
}

func kernelJSON(ctx context.Context, path string, args ...string) (any, error) {
	b, e := command(ctx, "", path, args...)
	if e != nil {
		return nil, e
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil, ErrDenied
	}
	return v, nil
}

func routeState(ctx context.Context) ([]byte, error) {
	var all []any
	for _, family := range []string{"-4", "-6"} {
		v, e := kernelJSON(ctx, "/usr/sbin/ip", family, "-j", "route", "show", "table", "all")
		if e != nil {
			return nil, e
		}
		rows, ok := v.([]any)
		if !ok {
			return nil, ErrDenied
		}
		for _, row := range rows {
			m, ok := row.(map[string]any)
			if !ok {
				return nil, ErrDenied
			}
			if m["dst"] == "default" || m["dst"] == "0.0.0.0/0" || m["dst"] == "::/0" || m["encap"] != nil || m["multipath"] != nil {
				return nil, ErrDenied
			}
		}
		rules, e := kernelJSON(ctx, "/usr/sbin/ip", family, "-j", "rule", "show")
		if e != nil {
			return nil, e
		}
		all = append(all, map[string]any{"routes": v, "rules": rules})
	}
	return canonical(all), nil
}

func addressState(ctx context.Context) ([]byte, error) {
	v, e := kernelJSON(ctx, "/usr/sbin/ip", "-j", "addr", "show")
	if e != nil {
		return nil, e
	}
	return canonical(v), nil
}

// Fingerprints is used by the trusted allocator before bootstrapping the
// management process. It never approves an upstream route or unknown transform.
func Fingerprints(ctx context.Context) (string, string, error) {
	r, e := routeState(ctx)
	if e != nil {
		return "", "", e
	}
	a, e := addressState(ctx)
	if e != nil {
		return "", "", e
	}
	return digest(r), digest(a), nil
}

func validConfig(c Config, now time.Time) bool {
	return c.Lineage.Valid() && (c.Role == "run" || c.Role == "gateway") &&
		strings.HasPrefix(c.Namespace, "net:[") && strings.HasSuffix(c.Namespace, "]") &&
		len(c.RouteHash) == 64 && len(c.AddressHash) == 64 && c.Probe4.Is4() && c.Probe6.Is6() && !c.Probe6.Is4In6() &&
		netip.MustParsePrefix("198.19.64.0/23").Contains(c.Probe4) && netip.MustParsePrefix("fd64:64::/32").Contains(c.Probe6) &&
		c.Host == "a.fixture.example.com" && c.HardDeadline.After(now) && !c.HardDeadline.After(now.Add(35*time.Second))
}

// Only these pre-registered addresses exist on the isolated receiver wire.
// A new production target cannot be passed as a configuration parameter.
func fixtureTarget(ip netip.Addr) bool {
	return ip == netip.MustParseAddr("93.184.216.34") || ip == netip.MustParseAddr("2001:4860:ffff::34")
}

func policy(c Config) string {
	sets := "set target4 { type ipv4_addr . inet_service; flags timeout; }\nset target6 { type ipv6_addr . inet_service; flags timeout; }\n" +
		"set resolver4 { type ipv4_addr . inet_service; flags timeout; }\nset resolver6 { type ipv6_addr . inet_service; flags timeout; }\n"
	out, in := "", ""
	for _, x := range []struct{ family, source, resolver, set string }{
		{"ip", c.Probe4.String(), "resolver4", "target4"}, {"ip6", c.Probe6.String(), "resolver6", "target6"},
	} {
		prefix, reverse := `oifname "eth0" `, `iifname "eth0" `
		if c.Role == "gateway" {
			prefix, reverse = `iifname "eth0" oifname "eth1" `, `iifname "eth1" oifname "eth0" `
		}
		out += fmt.Sprintf("%s%s saddr %s %s daddr . tcp dport @%s counter accept\n", prefix, x.family, x.source, x.family, x.resolver)
		// The first target SYN consumes the namespace's single connection burst.
		// Only that conntrack entry is marked; a reconnect (even with a reused
		// source port) cannot inherit it. The one/day refill exceeds the <=35s
		// namespace lifetime. Every marked packet still needs the timed target.
		target := fmt.Sprintf("%s%s saddr %s %s daddr . tcp dport @%s ", prefix, x.family, x.source, x.family, x.set)
		out += target + "ct mark 1 counter accept\n"
		out += target + "ct state new tcp flags & (syn | ack) == syn limit rate 1/day burst 1 packets ct mark set 1 counter accept\n"
		in += fmt.Sprintf("%s%s daddr %s %s saddr . tcp sport @%s counter accept\n", reverse, x.family, x.source, x.family, x.resolver)
		in += fmt.Sprintf("%s%s daddr %s %s saddr . tcp sport @%s ct mark 1 counter accept\n", reverse, x.family, x.source, x.family, x.set)
	}
	chain := func(name, hook, body string) string {
		return fmt.Sprintf("chain %s { type filter hook %s priority -150; policy drop;\n%s counter drop\n}\n", name, hook, body)
	}
	rules := chain("input", "input", "") + chain("output", "output", "") + chain("forward", "forward", out+in)
	if c.Role == "run" {
		rules = chain("input", "input", in) + chain("output", "output", out) + chain("forward", "forward", "")
	}
	return "table inet dp_run {\n" + sets + rules + "}\n"
}

// Strip only kernel runtime counters/metainfo and expiring permit elements.
// All hooks, policies, rule expressions, other sets and NAT objects remain in
// the audit hash. Before installation the target sets must also be empty.
func stable(v any) any {
	switch x := v.(type) {
	case map[string]any:
		y := map[string]any{}
		for k, z := range x {
			if k == "metainfo" || k == "handle" || k == "packets" || k == "bytes" {
				continue
			}
			if k == "elem" && (x["name"] == "target4" || x["name"] == "target6") {
				continue
			}
			if x["val"] != nil && (k == "timeout" || k == "expires") {
				continue
			}
			y[k] = stable(z)
		}
		return y
	case []any:
		y := []any{}
		for _, z := range x {
			m, ok := z.(map[string]any)
			if ok && m["metainfo"] != nil {
				continue
			}
			y = append(y, stable(z))
		}
		return y
	default:
		return v
	}
}

type Policy struct {
	c        Config
	ready    Ready
	hardMono time.Time
	used     atomic.Bool
	target   targetpermit.Request
}

func Boot(ctx context.Context, c Config) (*Policy, error) {
	now := time.Now()
	if !validConfig(c, now) || os.Geteuid() != 0 {
		return nil, ErrDenied
	}
	status, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return nil, ErrDenied
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(status), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[k] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"CapEff", "CapPrm", "CapBnd"} {
		if fields[k] != "0000000000001000" {
			return nil, ErrDenied
		}
	}
	if fields["CapInh"] != "0000000000000000" || fields["CapAmb"] != "0000000000000000" || fields["NoNewPrivs"] != "1" {
		return nil, ErrDenied
	}
	ns, e := os.Readlink("/proc/self/ns/net")
	if e != nil || ns != c.Namespace {
		return nil, ErrDenied
	}
	p := &Policy{c: c, hardMono: now.Add(c.HardDeadline.Sub(now))}
	if p.topology(ctx) != nil {
		return nil, ErrDenied
	}
	// The per-management-container private tmpfs is never shared with Probe.
	// Burn bootstrap before touching rules; deleting a rule table does not allow
	// a restarted process to renew this namespace's execution capability.
	f, e := os.OpenFile("/tmp/devpilot-network64.boot", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, ErrDenied
	}
	_, writeErr := f.Write(canonical(map[string]string{"namespace": c.Namespace, "lineage_hash": digest(canonical(c.Lineage))}))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return nil, ErrDenied
	}
	v, e := kernelJSON(ctx, "/usr/sbin/nft", "-j", "list", "ruleset")
	if e != nil {
		return nil, e
	}
	// A restarted process cannot flush the one-shot namespace marker and renew
	// an old capability. Only Docker's initial isolated DNS NAT may be removed.
	object, ok := v.(map[string]any)
	if !ok {
		return nil, ErrDenied
	}
	rows, ok := object["nftables"].([]any)
	if !ok {
		return nil, ErrDenied
	}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return nil, ErrDenied
		}
		for k, z := range m {
			if k == "metainfo" {
				continue
			}
			o, ok := z.(map[string]any)
			if !ok {
				return nil, ErrDenied
			}
			name := o["table"]
			if k == "table" {
				name = o["name"]
			}
			if o["family"] != "ip" || name != "nat" {
				return nil, ErrDenied
			}
		}
	}
	remaining := time.Until(p.hardMono)
	if wall := time.Until(c.HardDeadline); wall < remaining {
		remaining = wall
	}
	ms := (remaining - kernelCommandBudget).Milliseconds()
	if ms < 1 {
		return nil, ErrDenied
	}
	bootstrap := fmt.Sprintf("add element inet dp_run resolver4 { 192.0.2.53 . 53 timeout %dms }\nadd element inet dp_run resolver6 { fd64:53::53 . 53 timeout %dms }\n", ms, ms)
	if _, e = command(ctx, "flush ruleset\n"+policy(c)+bootstrap, "/usr/sbin/nft", "-f", "-"); e != nil {
		return nil, e
	}
	h, e := p.ruleHash(ctx, true)
	if e != nil {
		return nil, e
	}
	p.ready = Ready{Namespace: ns, Role: c.Role, LineageHash: digest(canonical(c.Lineage)), RouteHash: c.RouteHash, AddressHash: c.AddressHash, RulesHash: h, HardDeadline: c.HardDeadline}
	return p, nil
}

func (p *Policy) topology(ctx context.Context) error {
	wantForward := "0"
	if p.c.Role == "gateway" {
		wantForward = "1"
	}
	for _, path := range []string{"/proc/sys/net/ipv4/ip_forward", "/proc/sys/net/ipv6/conf/all/forwarding"} {
		b, e := os.ReadFile(path)
		if e != nil || strings.TrimSpace(string(b)) != wantForward {
			return ErrDenied
		}
	}
	if b, e := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6"); e != nil || strings.TrimSpace(string(b)) != "0" {
		return ErrDenied
	}
	r, a, e := Fingerprints(ctx)
	if e != nil || r != p.c.RouteHash || a != p.c.AddressHash {
		return ErrDenied
	}
	v, e := kernelJSON(ctx, "/usr/sbin/ip", "-j", "-d", "link", "show")
	if e != nil {
		return e
	}
	rows, ok := v.([]any)
	if !ok {
		return ErrDenied
	}
	up := map[string]bool{}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return ErrDenied
		}
		flags, _ := m["flags"].([]any)
		for _, f := range flags {
			if f == "UP" {
				name, _ := m["ifname"].(string)
				if name != "lo" {
					info, _ := m["linkinfo"].(map[string]any)
					if info["info_kind"] != "veth" {
						return ErrDenied
					}
				}
				up[name] = true
			}
		}
	}
	if !up["lo"] || !up["eth0"] || len(up) != 2 && p.c.Role == "run" || len(up) != 3 && p.c.Role == "gateway" || p.c.Role == "gateway" && !up["eth1"] {
		return ErrDenied
	}
	return nil
}

func (p *Policy) ruleHash(ctx context.Context, empty bool) (string, error) {
	v, e := kernelJSON(ctx, "/usr/sbin/nft", "-j", "list", "ruleset")
	if e != nil {
		return "", e
	}
	object, ok := v.(map[string]any)
	if !ok {
		return "", ErrDenied
	}
	rows, ok := object["nftables"].([]any)
	if !ok {
		return "", ErrDenied
	}
	counts := map[string]int{}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return "", ErrDenied
		}
		set, ok := m["set"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := set["name"].(string)
		if name != "resolver4" && name != "resolver6" && name != "target4" && name != "target6" {
			continue
		}
		entries, _ := set["elem"].([]any)
		for _, entry := range entries {
			em, ok := entry.(map[string]any)
			if !ok {
				return "", ErrDenied
			}
			elem, ok := em["elem"].(map[string]any)
			if !ok {
				return "", ErrDenied
			}
			val, ok := elem["val"].(map[string]any)
			if !ok {
				return "", ErrDenied
			}
			parts, ok := val["concat"].([]any)
			if !ok || len(parts) != 2 {
				return "", ErrDenied
			}
			ip, _ := parts[0].(string)
			port, _ := parts[1].(float64)
			expires, ok := elem["expires"].(float64)
			if !ok || expires < 0 || expires > 35 {
				return "", ErrDenied
			}
			bound := p.c.HardDeadline
			if name == "resolver4" {
				if ip != "192.0.2.53" || port != 53 {
					return "", ErrDenied
				}
			}
			if name == "resolver6" {
				if ip != "fd64:53::53" || port != 53 {
					return "", ErrDenied
				}
			}
			if strings.HasPrefix(name, "target") {
				if empty || p.target.Deadline.IsZero() || ip != p.target.IP.String() || port != float64(p.target.Port) {
					return "", ErrDenied
				}
				if p.target.Deadline.Before(bound) {
					bound = p.target.Deadline
				}
			}
			// JSON nft expiries are rounded to seconds. Include a full second
			// to bound them conservatively instead of ignoring TTL drift.
			if time.Now().Add(time.Duration((expires + 1) * float64(time.Second))).After(bound) {
				return "", ErrDenied
			}
			counts[name]++
		}
	}
	if counts["resolver4"] != 1 || counts["resolver6"] != 1 || empty && counts["target4"]+counts["target6"] != 0 || !empty && counts["target4"]+counts["target6"] != 1 {
		return "", ErrDenied
	}
	if empty {
		var walk func(any) bool
		walk = func(v any) bool {
			switch x := v.(type) {
			case map[string]any:
				if (x["name"] == "target4" || x["name"] == "target6") && x["elem"] != nil {
					return false
				}
				for _, z := range x {
					if !walk(z) {
						return false
					}
				}
			case []any:
				for _, z := range x {
					if !walk(z) {
						return false
					}
				}
			}
			return true
		}
		if !walk(v) {
			return "", ErrDenied
		}
	}
	return digest(canonical(stable(v))), nil
}

func (p *Policy) Ready() Ready { return p.ready }

func (p *Policy) Install(ctx context.Context, x Install) (targetpermit.Confirmation, error) {
	if p == nil || !p.used.CompareAndSwap(false, true) {
		return targetpermit.Confirmation{}, errConsumed
	}
	r := x.Request
	if !r.Valid() || r.Lineage != p.c.Lineage || r.Host != p.c.Host || !fixtureTarget(r.IP) || r.Deadline.After(p.c.HardDeadline) || !p.hardMono.After(time.Now()) ||
		x.CommittedAt.IsZero() || x.PermitDeadline.Before(r.Deadline) || !validPermitID(x.PermitID) || ctx.Err() != nil {
		return targetpermit.Confirmation{}, errBinding
	}
	if x.CommittedAt.After(time.Now()) {
		return targetpermit.Confirmation{}, errFutureProof
	}
	if p.topology(ctx) != nil {
		return targetpermit.Confirmation{}, errTopology
	}
	h, e := p.ruleHash(ctx, true)
	if e != nil || h != p.ready.RulesHash {
		return targetpermit.Confirmation{}, errRules
	}
	remaining := time.Until(r.Deadline)
	if hard := time.Until(p.hardMono); hard < remaining {
		remaining = hard
	}
	// nft timeout is relative to kernel insertion, not the earlier userspace
	// calculation. Reserve the entire bounded command interval so delayed
	// insertion cannot extend the fixed deadline. Short permits fail closed.
	ms := (remaining - kernelCommandBudget).Milliseconds()
	if ms < 1 {
		return targetpermit.Confirmation{}, errExpiry
	}
	set := "target4"
	if r.IP.Is6() {
		set = "target6"
	}
	script := fmt.Sprintf("add element inet dp_run %s { %s . %d timeout %dms }\n", set, r.IP, r.Port, ms)
	p.target = r
	if _, e = command(ctx, script, "/usr/sbin/nft", "-f", "-"); e != nil {
		return targetpermit.Confirmation{}, errKernel
	}
	h, e = p.ruleHash(ctx, false)
	if e != nil || h != p.ready.RulesHash || !r.Deadline.After(time.Now()) || !p.hardMono.After(time.Now()) || ctx.Err() != nil {
		return targetpermit.Confirmation{}, errRules
	}
	return targetpermit.Confirmation{Request: r, PermitID: x.PermitID, Deadline: r.Deadline}, nil
}

func validPermitID(s string) bool {
	// Reuse the exact UUID contract without exporting a general permit minting API.
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range []byte(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Decode rejects ambiguous JSON at the management boundary. The command reads
// at most one bounded configuration and one bounded install; no reconnect API.
func Decode(b []byte, v any) error {
	if len(b) > 4096 || !credentials.ValidJSON(b) {
		return ErrDenied
	}
	if uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0) != nil {
		return ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrDenied
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrDenied
	}
	return nil
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrDenied
	}
	t, e := d.Token()
	if e != nil {
		return ErrDenied
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			s, ok := k.(string)
			if e != nil || !ok || seen[s] || len(seen) >= 64 {
				return ErrDenied
			}
			seen[s] = true
			if uniqueJSON(d, depth+1) != nil {
				return ErrDenied
			}
		}
		t, e = d.Token()
		if e != nil || t != json.Delim('}') {
			return ErrDenied
		}
	case json.Delim('['):
		for d.More() {
			if uniqueJSON(d, depth+1) != nil {
				return ErrDenied
			}
		}
		t, e = d.Token()
		if e != nil || t != json.Delim(']') {
			return ErrDenied
		}
	}
	return nil
}
