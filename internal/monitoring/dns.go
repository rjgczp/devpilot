package monitoring

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/monitor"
	"golang.org/x/net/dns/dnsmessage"
)

const (
	dnsMessageLimit = 16 << 10
	dnsTotalLimit   = 64 << 10
	dnsRRLimit      = 128
	dnsQueryLimit   = 18
	dnsAliasLimit   = 8
	dnsAddressLimit = 32
)

type dnsResolver struct {
	address netip.Addr
	policy  addressPolicy
}
type dnsSession struct {
	conn                    net.Conn
	total, records, queries int
	policy                  addressPolicy
}
type familyAnswer struct {
	chain     []string
	addresses []netip.Addr
}

func classify(ctx context.Context, err error, fallback Failure) Failure {
	var f failureError
	if errors.As(err, &f) {
		return f.code
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return Canceled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Timeout
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return Timeout
	}
	return fallback
}
func networkFor(ip netip.Addr) string {
	if ip.Is4() {
		return "tcp4"
	}
	return "tcp6"
}
func remoteMatches(conn net.Conn, approved netip.AddrPort) bool {
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	return ok && addr.AddrPort() == approved
}

func (r dnsResolver) resolve(parent context.Context, host string) ([]netip.Addr, error) {
	if !r.address.IsValid() || r.address.Is4In6() || r.address.Zone() != "" || !monitor.CanonicalTargetName(host) {
		return nil, fail(BlockedTarget)
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	endpoint := netip.AddrPortFrom(r.address, 53)
	conn, err := (&net.Dialer{}).DialContext(ctx, networkFor(r.address), endpoint.String())
	if err != nil {
		return nil, fail(classify(ctx, err, DNSFailed))
	}
	defer conn.Close()
	if !remoteMatches(conn, endpoint) {
		return nil, fail(DNSFailed)
	}
	deadline, _ := ctx.Deadline()
	if conn.SetDeadline(deadline) != nil {
		return nil, fail(DNSFailed)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	session := dnsSession{conn: conn, policy: r.policy}
	a, err := session.family(ctx, host, dnsmessage.TypeA)
	if err != nil {
		return nil, err
	}
	aaaa, err := session.family(ctx, host, dnsmessage.TypeAAAA)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(a.chain, aaaa.chain) {
		return nil, fail(DNSFailed)
	}
	set := map[netip.Addr]bool{}
	for _, ip := range append(a.addresses, aaaa.addresses...) {
		set[ip] = true
	}
	if len(set) == 0 || len(set) > dnsAddressLimit {
		return nil, fail(DNSFailed)
	}
	addresses := make([]netip.Addr, 0, len(set))
	for ip := range set {
		addresses = append(addresses, ip)
	}
	slices.SortFunc(addresses, func(a, b netip.Addr) int {
		if a.Is4() != b.Is4() {
			if a.Is4() {
				return -1
			}
			return 1
		}
		return a.Compare(b)
	})
	return addresses, nil
}

func canonicalDNSName(n dnsmessage.Name) (string, bool) {
	s := strings.ToLower(n.String())
	if !strings.HasSuffix(s, ".") {
		return "", false
	}
	s = strings.TrimSuffix(s, ".")
	return s, monitor.CanonicalTargetName(s)
}

func (s *dnsSession) family(ctx context.Context, host string, typ dnsmessage.Type) (familyAnswer, error) {
	out := familyAnswer{chain: []string{host}}
	current := host
	seen := map[string]bool{host: true}
	for {
		msg, err := s.exchange(ctx, current, typ)
		if err != nil {
			return out, err
		}
		aliases := map[string]string{}
		addresses := map[string][]netip.Addr{}
		hasAddress := map[string]bool{}
		all := append(append(append([]dnsmessage.Resource(nil), msg.Answers...), msg.Authorities...), msg.Additionals...)
		// Inspect every received address, including records we will never use.
		for _, rr := range all {
			if rr.Header.Type == dnsmessage.Type(39) {
				return out, fail(DNSFailed)
			}
			var ip netip.Addr
			switch body := rr.Body.(type) {
			case *dnsmessage.AResource:
				ip = netip.AddrFrom4(body.A)
			case *dnsmessage.AAAAResource:
				ip = netip.AddrFrom16(body.AAAA)
			}
			if ip.IsValid() && !s.policy.allowed(ip) {
				return out, fail(BlockedTarget)
			}
			if body, ok := rr.Body.(*dnsmessage.CNAMEResource); ok {
				if _, valid := canonicalDNSName(body.CNAME); !valid {
					return out, fail(BlockedTarget)
				}
			}
		}
		for _, rr := range msg.Answers {
			owner, ok := canonicalDNSName(rr.Header.Name)
			if rr.Header.Class != dnsmessage.ClassINET {
				return out, fail(DNSFailed)
			}
			switch body := rr.Body.(type) {
			case *dnsmessage.CNAMEResource:
				if !ok {
					return out, fail(BlockedTarget)
				}
				target, valid := canonicalDNSName(body.CNAME)
				if !valid {
					return out, fail(BlockedTarget)
				}
				if old, exists := aliases[owner]; exists && old != target {
					return out, fail(DNSFailed)
				}
				aliases[owner] = target
			case *dnsmessage.AResource:
				if !ok {
					return out, fail(DNSFailed)
				}
				hasAddress[owner] = true
				if typ == dnsmessage.TypeA {
					addresses[owner] = append(addresses[owner], netip.AddrFrom4(body.A))
				}
			case *dnsmessage.AAAAResource:
				if !ok {
					return out, fail(DNSFailed)
				}
				hasAddress[owner] = true
				if typ == dnsmessage.TypeAAAA {
					addresses[owner] = append(addresses[owner], netip.AddrFrom16(body.AAAA))
				}
			}
		}
		for owner := range aliases {
			if hasAddress[owner] {
				return out, fail(DNSFailed)
			}
		}
		before := current
		for aliases[current] != "" {
			current = aliases[current]
			if seen[current] || len(out.chain) > dnsAliasLimit {
				return out, fail(DNSFailed)
			}
			seen[current] = true
			out.chain = append(out.chain, current)
		}
		if ips := addresses[current]; len(ips) > 0 {
			out.addresses = ips
			return out, nil
		}
		if current == before {
			return out, nil
		} // complete NOERROR/NODATA for this family
		// Alias-only answer: explicitly obtain the terminal family RRset.
	}
}

func (s *dnsSession) exchange(ctx context.Context, host string, typ dnsmessage.Type) (dnsmessage.Message, error) {
	var empty dnsmessage.Message
	if s.queries >= dnsQueryLimit {
		return empty, fail(DNSFailed)
	}
	s.queries++
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return empty, fail(DNSFailed)
	}
	var random [2]byte
	if _, err = rand.Read(random[:]); err != nil {
		return empty, fail(DNSFailed)
	}
	question := dnsmessage.Question{Name: name, Type: typ, Class: dnsmessage.ClassINET}
	q := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(random[:]), RecursionDesired: true}, Questions: []dnsmessage.Question{question}}
	wire, err := q.Pack()
	if err != nil || s.total+len(wire)+4 > dnsTotalLimit {
		return empty, fail(DNSFailed)
	}
	s.total += len(wire) + 2
	packet := make([]byte, 2, len(wire)+2)
	binary.BigEndian.PutUint16(packet, uint16(len(wire)))
	packet = append(packet, wire...)
	if _, err = io.Copy(s.conn, strings.NewReader(string(packet))); err != nil {
		return empty, fail(classify(ctx, err, DNSFailed))
	}
	var size [2]byte
	if _, err = io.ReadFull(s.conn, size[:]); err != nil {
		return empty, fail(classify(ctx, err, DNSFailed))
	}
	n := int(binary.BigEndian.Uint16(size[:]))
	if n < 12 || n > dnsMessageLimit || s.total+n+2 > dnsTotalLimit {
		return empty, fail(DNSFailed)
	}
	s.total += n + 2
	wire = make([]byte, n)
	if _, err = io.ReadFull(s.conn, wire); err != nil {
		return empty, fail(classify(ctx, err, DNSFailed))
	}
	count := int(binary.BigEndian.Uint16(wire[6:8])) + int(binary.BigEndian.Uint16(wire[8:10])) + int(binary.BigEndian.Uint16(wire[10:12]))
	if s.records+count > dnsRRLimit || !completeDNSWire(wire) {
		return empty, fail(DNSFailed)
	}
	s.records += count
	var msg dnsmessage.Message
	if msg.Unpack(wire) != nil || !msg.Response || msg.ID != q.ID || msg.OpCode != 0 || msg.Truncated || msg.RCode != dnsmessage.RCodeSuccess || len(msg.Questions) != 1 || msg.Questions[0] != question {
		return empty, fail(DNSFailed)
	}
	return msg, nil
}

// dnsmessage does not expose its final offset. This bounded framing walk also
// rejects trailing data before the library validates names/compression/RDATA.
func completeDNSWire(b []byte) bool {
	if len(b) < 12 || binary.BigEndian.Uint16(b[4:6]) != 1 {
		return false
	}
	off := 12
	name := func() bool {
		for steps := 0; steps < 128; steps++ {
			if off >= len(b) {
				return false
			}
			c := b[off]
			off++
			if c == 0 {
				return true
			}
			if c&0xc0 == 0xc0 {
				if off >= len(b) {
					return false
				}
				target := int(c&0x3f)<<8 | int(b[off])
				off++
				return target >= 12 && target < off-2
			}
			if c&0xc0 != 0 || int(c) > len(b)-off {
				return false
			}
			off += int(c)
		}
		return false
	}
	if !name() || off+4 > len(b) {
		return false
	}
	off += 4
	count := int(binary.BigEndian.Uint16(b[6:8])) + int(binary.BigEndian.Uint16(b[8:10])) + int(binary.BigEndian.Uint16(b[10:12]))
	if count > dnsRRLimit {
		return false
	}
	for i := 0; i < count; i++ {
		if !name() || off+10 > len(b) {
			return false
		}
		n := int(binary.BigEndian.Uint16(b[off+8 : off+10]))
		off += 10
		if n > len(b)-off {
			return false
		}
		off += n
	}
	return off == len(b)
}
