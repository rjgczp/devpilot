package monitoring

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestPinnedRegistryPolicy(t *testing.T) {
	var manifest map[string]struct {
		SHA256 string `json:"sha256"`
	}
	b, _ := registryFiles.ReadFile("registry/manifest.json")
	if json.Unmarshal(b, &manifest) != nil {
		t.Fatal("manifest")
	}
	for name, item := range manifest {
		b, _ := registryFiles.ReadFile("registry/" + name + ".csv")
		if fmt.Sprintf("%x", sha256.Sum256(b)) != item.SHA256 {
			t.Fatal("registry modified", name)
		}
	}
	p, err := policyWithDeny([]netip.Prefix{netip.MustParsePrefix("8.8.4.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111", "2001:4860:4860::8888"} {
		if !p.allowed(netip.MustParseAddr(s)) {
			t.Fatal("ordinary allocated", s)
		}
	}
	for _, s := range []string{"0.1.2.3", "10.0.0.1", "100.64.0.0", "100.127.255.255", "100.100.100.200", "127.0.0.1", "169.254.169.254", "172.16.0.1", "192.168.1.1", "192.0.0.9", "192.88.99.1", "198.18.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255", "8.8.4.4", "::", "::1", "::ffff:8.8.8.8", "::ffff:127.0.0.1", "::8.8.8.8", "64:ff9b::808:808", "64:ff9b:1::1", "2001::1", "2001:db8::1", "2002:0808:0808::1", "3fff::1", "fc00::1", "fe80::1", "fec0::1", "ff02::1", "2606:4700::5efe:808:808", "2606:4700:0:0:200:5efe:808:808", "4000::1", "2606:4700::1%bad"} {
		if p.allowed(netip.MustParseAddr(s)) {
			t.Fatal("unsafe", s)
		}
	}
	if !netip.MustParseAddr("10.1.1.1").IsGlobalUnicast() {
		t.Fatal("toolchain counterexample changed")
	}
	// Every pinned special-purpose prefix is denied at both boundaries.
	for _, prefix := range p.denied {
		first := prefix.Masked().Addr()
		last := prefixLast(prefix)
		if p.allowed(first) || p.allowed(last) {
			t.Fatal("prefix boundary", prefix)
		}
		// Neighbor calls must be total and cannot alias mapped representations.
		_ = p.allowed(first.Prev())
		_ = p.allowed(last.Next())
	}
}
func prefixLast(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr()
	if a.Is4() {
		b := a.As4()
		for i := p.Bits(); i < 32; i++ {
			b[i/8] |= 1 << uint(7-i%8)
		}
		return netip.AddrFrom4(b)
	}
	b := a.As16()
	for i := p.Bits(); i < 128; i++ {
		b[i/8] |= 1 << uint(7-i%8)
	}
	return netip.AddrFrom16(b)
}
func FuzzAddressPolicy(f *testing.F) {
	for _, s := range []string{"8.8.8.8", "::ffff:10.0.0.1", "2606:4700::5efe:a00:1", "64:ff9b::a00:1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		ip, e := netip.ParseAddr(s)
		if e != nil {
			return
		}
		p, _ := policyWithDeny(nil)
		if p.allowed(ip) && (ip.IsPrivate() || ip.Is4In6() || ip.Zone() != "") {
			t.Fatal("unsafe representation")
		}
	})
}

func TestAdmissionAtomicLimitsRetentionAndCapacity(t *testing.T) {
	now := time.Now()
	g := &admissionGate{}
	s := testSnapshot(0)
	r, ok := g.acquire(s, now)
	if !ok {
		t.Fatal("admit")
	}
	before := g.global.tokens
	if _, ok = g.acquire(s, now); ok || g.global.tokens != before {
		t.Fatal("duplicate consumed tokens")
	}
	r()
	r()
	s.interval = 3600
	if _, ok = g.acquire(s, now.Add(61*time.Second)); ok {
		t.Fatal("interval growth lost retained start")
	}
	s.interval = 60
	s.version++
	if _, ok = g.acquire(s, now.Add(59*time.Second)); ok {
		t.Fatal("version bypass")
	}
	if r, ok = g.acquire(s, now.Add(time.Hour)); !ok {
		t.Fatal("expiry")
	} else {
		r()
	}
	// Independent workspaces/projects fill exactly 32; per tenant at most four.
	g = &admissionGate{}
	var releases []func()
	for i := 0; i < 32; i++ {
		s := testSnapshot(i)
		s.scope.WorkspaceID = fmt.Sprintf("01900000-0000-7000-8000-%012x", 100+i/4)
		s.scope.ProjectID = fmt.Sprintf("01900000-0000-7000-8000-%012x", 200+i/2)
		r, ok := g.acquire(s, now)
		if !ok {
			t.Fatal("global fill", i)
		}
		releases = append(releases, r)
	}
	if _, ok = g.acquire(testSnapshot(99), now); ok {
		t.Fatal("global limit")
	}
	for _, r := range releases {
		r()
	}
	if g.active != 0 {
		t.Fatal("leaked slots")
	}
	g = &admissionGate{}
	for i := 0; i < 4; i++ {
		s := testSnapshot(i)
		s.scope.ProjectID = fmt.Sprintf("01900000-0000-7000-8000-%012x", 100+i/2)
		if _, ok = g.acquire(s, now); !ok {
			t.Fatal("tenant fill")
		}
	}
	if _, ok = g.acquire(testSnapshot(99), now); ok {
		t.Fatal("workspace limit")
	}
	other := testSnapshot(99)
	other.scope.WorkspaceID = testScope(999).MonitorID
	if r, ok := g.acquire(other, now); !ok {
		t.Fatal("tenant isolation")
	} else {
		r()
	}
	g = &admissionGate{records: map[string]gateRecord{}}
	for i := 0; i < maxGateRecords; i++ {
		g.records[fmt.Sprint(i)] = gateRecord{retainUntil: now.Add(time.Hour)}
	}
	if _, ok = g.acquire(testSnapshot(0), now); ok || g.global.tokens != 0 {
		t.Fatal("capacity consumption")
	}
	if len(g.records) != maxGateRecords {
		t.Fatal("unexpired eviction")
	}
}
func TestAdmissionRaceReleaseAndRates(t *testing.T) {
	g := &admissionGate{}
	now := time.Now()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, ok := g.acquire(testSnapshot(1), now); ok {
				accepted.Add(1)
				r()
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || g.active != 0 {
		t.Fatal("concurrent gate")
	}
	g = &admissionGate{}
	for i := 0; i < 2; i++ {
		r, ok := g.acquire(testSnapshot(i), now)
		if !ok {
			t.Fatal("burst")
		}
		r()
	}
	before := g.global.tokens
	if _, ok := g.acquire(testSnapshot(2), now); ok || g.global.tokens != before {
		t.Fatal("project rate atomicity")
	}
	if r, ok := g.acquire(testSnapshot(2), now.Add(time.Second)); !ok {
		t.Fatal("refill")
	} else {
		r()
	}
}

func dnsReply(q dnsmessage.Message, answers []dnsmessage.Resource) []byte {
	q.Response = true
	q.RecursionAvailable = true
	q.Answers = answers
	b, _ := q.Pack()
	return b
}
func addrRR(owner, ip string) dnsmessage.Resource {
	name, _ := dnsmessage.NewName(owner + ".")
	a := netip.MustParseAddr(ip)
	h := dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET}
	if a.Is4() {
		h.Type = dnsmessage.TypeA
		return dnsmessage.Resource{Header: h, Body: &dnsmessage.AResource{A: a.As4()}}
	}
	h.Type = dnsmessage.TypeAAAA
	return dnsmessage.Resource{Header: h, Body: &dnsmessage.AAAAResource{AAAA: a.As16()}}
}
func aliasRR(owner, target string) dnsmessage.Resource {
	name, _ := dnsmessage.NewName(owner + ".")
	cn, _ := dnsmessage.NewName(target + ".")
	return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET, Type: dnsmessage.TypeCNAME}, Body: &dnsmessage.CNAMEResource{CNAME: cn}}
}
func sessionWithReplies(t *testing.T, reply func(dnsmessage.Message) []byte) *dnsSession {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	_ = a.SetDeadline(time.Now().Add(time.Second))
	go func() {
		defer b.Close()
		for {
			var n [2]byte
			if _, e := io.ReadFull(b, n[:]); e != nil {
				return
			}
			buf := make([]byte, binary.BigEndian.Uint16(n[:]))
			if _, e := io.ReadFull(b, buf); e != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(buf) != nil {
				return
			}
			out := reply(q)
			binary.BigEndian.PutUint16(n[:], uint16(len(out)))
			if _, e := b.Write(append(n[:], out...)); e != nil {
				return
			}
		}
	}()
	p, _ := policyWithDeny(nil)
	return &dnsSession{conn: a, policy: p}
}
func TestDNSFamilyProtocolMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers []dnsmessage.Resource
		code    Failure
	}{
		{"safe", []dnsmessage.Resource{addrRR("probe-a.example.com", "93.184.216.34")}, ""},
		{"mixed-last", []dnsmessage.Resource{addrRR("probe-a.example.com", "93.184.216.34"), addrRR("probe-a.example.com", "10.0.0.1")}, BlockedTarget},
		{"mapped", []dnsmessage.Resource{addrRR("probe-a.example.com", "::ffff:8.8.8.8")}, BlockedTarget},
		{"loop", []dnsmessage.Resource{aliasRR("probe-a.example.com", "probe-a.example.com")}, DNSFailed},
		{"coexist", []dnsmessage.Resource{aliasRR("probe-a.example.com", "other.example.com"), addrRR("probe-a.example.com", "8.8.8.8")}, DNSFailed},
		{"conflict", []dnsmessage.Resource{aliasRR("probe-a.example.com", "a.example.com"), aliasRR("probe-a.example.com", "b.example.com")}, DNSFailed},
		{"reserved-alias", []dnsmessage.Resource{aliasRR("probe-a.example.com", "internal.local")}, BlockedTarget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sessionWithReplies(t, func(q dnsmessage.Message) []byte { return dnsReply(q, tc.answers) })
			_, err := s.family(context.Background(), "probe-a.example.com", dnsmessage.TypeA)
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if classify(context.Background(), err, ProtocolError) != tc.code {
				t.Fatal("classification", err)
			}
		})
	}
	for _, kind := range []string{"id", "question", "rcode", "truncated", "trailing", "additional-danger", "empty", "chain"} {
		t.Run(kind, func(t *testing.T) {
			s := sessionWithReplies(t, func(q dnsmessage.Message) []byte {
				answers := []dnsmessage.Resource{addrRR("probe-a.example.com", "8.8.8.8")}
				switch kind {
				case "id":
					q.ID++
				case "question":
					q.Questions[0].Type = dnsmessage.TypeAAAA
				case "rcode":
					q.RCode = dnsmessage.RCodeNameError
				case "truncated":
					q.Truncated = true
				case "additional-danger":
					q.Additionals = []dnsmessage.Resource{addrRR("other.example.com", "169.254.169.254")}
				case "empty":
					answers = nil
				case "chain":
					host := strings.TrimSuffix(q.Questions[0].Name.String(), ".")
					if host == "probe-a.example.com" {
						answers = []dnsmessage.Resource{aliasRR(host, "other.example.com")}
					} else {
						answers = []dnsmessage.Resource{addrRR(host, "8.8.8.8")}
					}
				}
				b := dnsReply(q, answers)
				if kind == "trailing" {
					b = append(b, 0)
				}
				return b
			})
			a, err := s.family(context.Background(), "probe-a.example.com", dnsmessage.TypeA)
			if kind == "empty" || kind == "chain" {
				if err != nil {
					t.Fatal(err)
				}
				if kind == "empty" && len(a.addresses) != 0 {
					t.Fatal("empty")
				}
			} else if err == nil {
				t.Fatal("malformed accepted")
			}
		})
	}
}
func TestDNSBudgetLimits(t *testing.T) {
	for _, which := range []string{"records", "total", "queries", "aliases", "message"} {
		t.Run(which, func(t *testing.T) {
			s := sessionWithReplies(t, func(q dnsmessage.Message) []byte {
				if which == "message" {
					return make([]byte, dnsMessageLimit+1)
				}
				if which == "aliases" {
					host := strings.TrimSuffix(q.Questions[0].Name.String(), ".")
					return dnsReply(q, []dnsmessage.Resource{aliasRR(host, "x."+host)})
				}
				return dnsReply(q, []dnsmessage.Resource{addrRR("probe-a.example.com", "8.8.8.8")})
			})
			switch which {
			case "records":
				s.records = dnsRRLimit
			case "total":
				s.total = dnsTotalLimit - 1
			case "queries":
				s.queries = dnsQueryLimit
			}
			if _, err := s.family(context.Background(), "probe-a.example.com", dnsmessage.TypeA); err == nil {
				t.Fatal("budget accepted")
			}
		})
	}
}
func FuzzDNSWire(f *testing.F) {
	q := dnsmessage.Message{Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("probe-a.example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	f.Add(dnsReply(q, []dnsmessage.Resource{addrRR("probe-a.example.com", "8.8.8.8")}))
	f.Add([]byte{0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > dnsMessageLimit {
			return
		}
		if completeDNSWire(b) {
			var m dnsmessage.Message
			_ = m.Unpack(b)
		}
	})
}

type memoryConn struct {
	io.Reader
	closed bool
	remote net.Addr
}

func (c *memoryConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *memoryConn) Close() error                     { c.closed = true; return nil }
func (c *memoryConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *memoryConn) RemoteAddr() net.Addr             { return c.remote }
func (c *memoryConn) SetDeadline(time.Time) error      { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error { return nil }
func TestPlaintextBudgetsBeforeParsing(t *testing.T) {
	var wireLimited atomic.Bool
	wire := &readBudget{Conn: &memoryConn{Reader: strings.NewReader(strings.Repeat("x", (128<<10)+1))}, remaining: 128 << 10, limited: &wireLimited}
	read, err := io.Copy(io.Discard, wire)
	if read != 128<<10 || err == nil || !wireLimited.Load() {
		t.Fatal("TLS ciphertext ceiling escaped")
	}
	for _, wire := range []string{"HTTP/1.1 200 OK\r\nX: " + strings.Repeat("x", 33<<10) + "\r\n\r\n", strings.Repeat("HTTP/1.1 103 Early Hints\r\n\r\n", 5)} {
		var limited atomic.Bool
		c := &httpInput{Conn: &memoryConn{Reader: strings.NewReader(wire)}, remaining: 64 << 10, limited: &limited}
		_, err := io.ReadAll(c)
		if !limited.Load() || err == nil {
			t.Fatal("limit")
		}
	}
	var limited atomic.Bool
	c := &httpInput{Conn: &memoryConn{Reader: strings.NewReader("HTTP/1.1 200 OK\r\nContent-Length: 99999999\r\n\r\n" + strings.Repeat("x", 100000))}, remaining: 64 << 10, limited: &limited}
	n, _ := io.Copy(io.Discard, c)
	if n > 64<<10 {
		t.Fatal("overread")
	}
	bad := &memoryConn{remote: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 80}}
	if remoteMatches(bad, netip.MustParseAddrPort("8.8.8.8:80")) {
		t.Fatal("remote mismatch")
	}
	if conn, err := pinConnection(bad, netip.MustParseAddrPort("8.8.8.8:80")); conn != nil || err == nil || !bad.closed {
		t.Fatal("mismatched connection escaped")
	}
	permit := dialPermit{origin: "probe-a.example.com:80"}
	if permit.take("tcp", "other.example.com:80") || permit.take("tcp6", "probe-a.example.com:80") || !permit.take("tcp", "probe-a.example.com:80") || permit.take("tcp", "probe-a.example.com:80") {
		t.Fatal("origin/second dial guard")
	}
	a := &activeConnection{}
	a.close()
	if a.install(bad) || !bad.closed {
		t.Fatal("late connection survived cancel")
	}
}

func TestMultipleKernelInstancesShareProcessAdmission(t *testing.T) {
	a, err := newEngine(netip.MustParseAddr("127.0.0.53"), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newEngine(netip.MustParseAddr("127.0.0.53"), nil)
	if err != nil || a.gate != b.gate {
		t.Fatal("per-instance quota bypass")
	}
	s := testSnapshot(501)
	release, ok := a.gate.acquire(s, time.Now())
	if !ok {
		t.Fatal("admit")
	}
	defer release()
	if r, err := b.execute(context.Background(), s); r != nil || !errors.Is(err, ErrResourceLimited) {
		t.Fatal("second constructor bypassed process limit")
	}
}
func TestSafePreflightAndClassification(t *testing.T) {
	p, _ := policyWithDeny(nil)
	e := &engine{resolver: dnsResolver{address: netip.MustParseAddr("127.0.0.1"), policy: p}}
	bad := testSnapshot(0)
	bad.target = "http://127.0.0.1/"
	if r, err := e.execute(context.Background(), bad); r != nil || !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatal("invalid configuration result")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := e.execute(ctx, testSnapshot(0)); r != nil || err == nil {
		t.Fatal("cancel preflight")
	}
	if classify(ctx, fail(BlockedTarget), DNSFailed) != BlockedTarget {
		t.Fatal("lost structured failure")
	}
	for _, f := range []Failure{DNSFailed, BlockedTarget, ConnectFailed, TLSFailed, Timeout, Canceled, ResponseLimit, ProtocolError, UnexpectedStatus} {
		if strings.Contains(fail(f).Error(), "sentinel") {
			t.Fatal("leak")
		}
	}
}
