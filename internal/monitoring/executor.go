package monitoring

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/textproto"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// All execution constructors/calls are private. Only controlled tests use them.
// No production command imports this package or obtains this capability.
type engine struct {
	resolver dnsResolver
	gate     *admissionGate
	roots    *x509.CertPool
}

var processAdmission admissionGate

func newEngine(resolver netip.Addr, deny []netip.Prefix) (*engine, error) {
	p, err := policyWithDeny(deny)
	if err != nil {
		return nil, err
	}
	if !resolver.IsValid() || resolver.Is4In6() || resolver.Zone() != "" {
		return nil, fail(BlockedTarget)
	}
	// Trust is the controlled OS image's trust store; no per-user certificate API.
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fail(TLSFailed)
	}
	return &engine{resolver: dnsResolver{resolver, p}, roots: roots, gate: &processAdmission}, nil
}

type activeConnection struct {
	mu     sync.Mutex
	conn   net.Conn
	closed bool
}

func (a *activeConnection) install(conn net.Conn) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		_ = conn.Close()
		return false
	}
	a.conn = conn
	return true
}
func (a *activeConnection) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	if a.conn != nil {
		_ = a.conn.Close()
	}
}

type dialPermit struct {
	origin    string
	attempted atomic.Bool
}

func (p *dialPermit) take(network, addr string) bool {
	return network == "tcp" && addr == p.origin && p.attempted.CompareAndSwap(false, true)
}
func pinConnection(conn net.Conn, endpoint netip.AddrPort) (net.Conn, error) {
	if !remoteMatches(conn, endpoint) {
		_ = conn.Close()
		return nil, fail(BlockedTarget)
	}
	return conn, nil
}

func (e *engine) execute(parent context.Context, s Snapshot) (*Result, error) {
	if !s.valid() {
		return nil, ErrInvalidConfiguration
	}
	if parent.Err() != nil {
		return nil, fail(classify(parent, parent.Err(), Canceled))
	}
	start := time.Now()
	release, ok := e.gate.acquire(s, start)
	if !ok {
		return nil, ErrResourceLimited
	}
	defer release()
	ctx, cancel := context.WithTimeout(parent, time.Duration(s.timeout)*time.Second)
	defer cancel()
	status, code := e.probe(ctx, s)
	return result(s, start, time.Now(), status, code), nil
}

func (e *engine) probe(ctx context.Context, s Snapshot) (*int, Failure) {
	u, err := url.Parse(s.target)
	if err != nil {
		return nil, ProtocolError
	}
	host := u.Hostname()
	port := uint16(80)
	if u.Scheme == "https" {
		port = 443
	}
	origin := net.JoinHostPort(host, strconv.Itoa(int(port)))
	permit := dialPermit{origin: origin}
	var active activeConnection
	stop := context.AfterFunc(ctx, active.close)
	defer stop()
	defer active.close()
	var limited atomic.Bool
	secureTCP := func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		if !permit.take(network, addr) {
			return nil, fail(BlockedTarget)
		}
		// Capture the probe context; Transport may detach its dial context.
		dctx, dcancel := context.WithCancel(ctx)
		defer dcancel()
		dstopped := context.AfterFunc(dialCtx, dcancel)
		defer dstopped()
		ips, err := e.resolver.resolve(dctx, host)
		if err != nil {
			return nil, err
		}
		ip := ips[0]
		if !e.resolver.policy.allowed(ip) {
			return nil, fail(BlockedTarget)
		}
		tctx, tcancel := context.WithTimeout(dctx, 3*time.Second)
		defer tcancel()
		endpoint := netip.AddrPortFrom(ip, port)
		conn, err := (&net.Dialer{}).DialContext(tctx, networkFor(ip), endpoint.String())
		if err != nil {
			return nil, fail(classify(tctx, err, ConnectFailed))
		}
		if !active.install(conn) {
			return nil, fail(classify(ctx, ctx.Err(), Canceled))
		}
		if _, err = pinConnection(conn, endpoint); err != nil {
			return nil, err
		}
		deadline, _ := ctx.Deadline()
		if conn.SetDeadline(deadline) != nil {
			_ = conn.Close()
			return nil, fail(ConnectFailed)
		}
		return conn, nil
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxConnsPerHost: 1,
		ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10, Protocols: protocols}
	transport.DialContext = func(c context.Context, n, a string) (net.Conn, error) {
		conn, err := secureTCP(c, n, a)
		if err != nil {
			return nil, err
		}
		return &httpInput{Conn: conn, remaining: 64 << 10, limited: &limited}, nil
	}
	transport.DialTLSContext = func(c context.Context, n, a string) (net.Conn, error) {
		conn, err := secureTCP(c, n, a)
		if err != nil {
			return nil, err
		}
		wire := &readBudget{Conn: conn, remaining: 128 << 10, limited: &limited}
		tc := tls.Client(wire, &tls.Config{ServerName: host, RootCAs: e.roots, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
		hctx, hcancel := context.WithTimeout(ctx, 3*time.Second)
		defer hcancel()
		if err = tc.HandshakeContext(hctx); err != nil {
			_ = tc.Close()
			return nil, fail(classify(hctx, err, TLSFailed))
		}
		// Retain the 128KiB wire ceiling after the handshake as well: otherwise
		// post-handshake TLS control/padding could consume unbounded ciphertext
		// without delivering any of the separately capped HTTP plaintext.
		return &httpInput{Conn: tc, remaining: 64 << 10, limited: &limited}, nil
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ProtocolError
	}
	req.Close = true
	informationals := 0
	req = req.WithContext(httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{Got1xxResponse: func(_ int, _ textproto.MIMEHeader) error {
		informationals++
		if informationals > 4 {
			limited.Store(true)
			return fail(ResponseLimit)
		}
		return nil
	}}))
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "DevPilot-Probe/1")
	resp, err := transport.RoundTrip(req)
	if err != nil {
		if limited.Load() {
			return nil, ResponseLimit
		}
		return nil, classify(ctx, err, ProtocolError)
	}
	// Close the socket BEFORE Body.Close: no client/drain implementation can
	// fetch additional response bytes after the header-only observation.
	active.close()
	_ = resp.Body.Close()
	status := resp.StatusCode
	if status < 200 || status > 599 {
		return nil, ProtocolError
	}
	if status != int(s.expected) {
		return &status, UnexpectedStatus
	}
	return &status, ""
}

type readBudget struct {
	net.Conn
	remaining int
	limited   *atomic.Bool
}

func (b *readBudget) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		b.limited.Store(true)
		return 0, fail(ResponseLimit)
	}
	if b.remaining > 0 && len(p) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.Conn.Read(p)
	if b.remaining > 0 {
		b.remaining -= n
	}
	return n, err
}

// httpInput bounds plaintext before HTTP parsing. It separately caps every
// header block, the informational count and total bytes including read-ahead.
// It does not expose/read Body or save response headers.
type httpInput struct {
	net.Conn
	remaining, headerBytes, informationals int
	first                                  [12]byte
	tail                                   uint32
	final                                  bool
	limited                                *atomic.Bool
}

func (h *httpInput) Read(p []byte) (int, error) {
	if h.remaining == 0 {
		h.limited.Store(true)
		return 0, fail(ResponseLimit)
	}
	if len(p) > h.remaining {
		p = p[:h.remaining]
	}
	n, err := h.Conn.Read(p)
	h.remaining -= n
	for _, c := range p[:n] {
		if h.final {
			break
		}
		if h.headerBytes < len(h.first) {
			h.first[h.headerBytes] = c
		}
		h.headerBytes++
		h.tail = h.tail<<8 | uint32(c)
		if h.tail == 0x0d0a0d0a {
			isInfo := h.first[9] == '1' && h.first[10] >= '0' && h.first[10] <= '9' && h.first[11] >= '0' && h.first[11] <= '9' && !(h.first[10] == '0' && h.first[11] == '1')
			if isInfo {
				h.informationals++
				h.headerBytes = 0
				h.first = [12]byte{}
				h.tail = 0
			} else {
				h.final = true
			}
			if h.informationals > 4 {
				h.limited.Store(true)
				return 0, fail(ResponseLimit)
			}
		}
		if h.headerBytes >= 32<<10 && !h.final {
			h.limited.Store(true)
			return 0, fail(ResponseLimit)
		}
	}
	return n, err
}
