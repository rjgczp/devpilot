//go:build executionnet && linux

package monitoring

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestExecutionPrepareTLS(t *testing.T) {
	if os.Getenv("EXECUTION_ISOLATED") != "1" || os.Geteuid() != 0 {
		t.Fatal("isolated privileged fixture setup required")
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable fixture CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile("/fixtures/ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644); err != nil {
		t.Fatal(err)
	}
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	derKey, _ := x509.MarshalECPrivateKey(leafKey)
	_ = os.WriteFile("/fixtures/server.key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: derKey}), 0600)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"*.example.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = os.WriteFile("/fixtures/server.pem", cert, 0644)
	_ = os.WriteFile("/fixtures/big.pem", []byte(strings.Repeat(string(cert), 400)), 0644)
	leaf.NotBefore = now.Add(-3 * time.Hour)
	leaf.NotAfter = now.Add(-2 * time.Hour)
	leaf.SerialNumber = big.NewInt(3)
	der, err = x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile("/fixtures/expired.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644)
}

func fixtureEvent(host, kind string) bool {
	b, _ := os.ReadFile("/evidence/events.jsonl")
	for _, line := range strings.Split(string(b), "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) == nil && row["host"] == host && row["kind"] == kind {
			return true
		}
	}
	return false
}
func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("observable fixture barrier not reached")
}

func TestExecutionProbe(t *testing.T) {
	if os.Getenv("EXECUTION_ISOLATED") != "1" || os.Geteuid() != 65534 {
		t.Fatal("unprivileged isolated probe required")
	}
	for _, name := range []string{"TEST_DATABASE_URL", "DATABASE_URL", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "COOKIE"} {
		if os.Getenv(name) != "" {
			t.Fatal("credential reached probe")
		}
	}
	status, _ := os.ReadFile("/proc/self/status")
	if !strings.Contains(string(status), "CapEff:\t0000000000000000") {
		t.Fatal("probe retained capabilities")
	}
	if !strings.Contains(string(status), "CapBnd:\t0000000000000000") || !strings.Contains(string(status), "NoNewPrivs:\t1") {
		t.Fatal("probe can regain privilege")
	}
	var limit syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit) != nil || limit.Cur > 256 {
		t.Fatal("FD hard limit missing")
	}
	caseName := os.Getenv("EXEC_CASE")
	if strings.HasPrefix(caseName, "bypass-") {
		addr := map[string]string{"bypass-v4": "169.254.169.254:80", "bypass-v6": "[fd00::1]:80", "bypass-nat": "[2001:4860:ffff::a9fe:a9fe]:80", "bypass-public": "93.184.216.36:80"}[caseName]
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: forbidden\r\n\r\n"))
			conn.Close()
			t.Fatal("firewall bypassed")
		}
		fmt.Println("CONTROL: deliberately attempted forbidden destination; classifier must reject this run")
		return
	}
	e, err := newEngine(netip.MustParseAddr("127.0.0.53"), []netip.Prefix{netip.MustParsePrefix("93.184.216.36/32"), netip.MustParsePrefix("2001:4860:ffff::/48")})
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile("/fixtures/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	e.roots = x509.NewCertPool()
	if !e.roots.AppendCertsFromPEM(ca) {
		t.Fatal("fixture CA")
	}
	if caseName == "concurrency" {
		realConcurrency(t, e)
		return
	}
	if caseName == "pg" {
		var w fixtureWire
		if json.NewDecoder(os.Stdin).Decode(&w) != nil {
			t.Fatal("controlled stdin snapshot")
		}
		s := w.snapshot()
		if !s.valid() {
			t.Fatal("invalid controlled snapshot")
		}
		r, err := e.execute(context.Background(), s)
		if err != nil || r == nil {
			t.Fatal("PG probe preflight")
		}
		got := Failure("")
		if r.FailureCode != nil {
			got = *r.FailureCode
		}
		if got != w.Want {
			t.Fatal("PG probe result category", got)
		}
		b, _ := json.Marshal(r)
		fmt.Println("RESULT:", string(b))
		return
	}
	s := testSnapshot(1)
	host := caseName + ".example.com"
	s.target = "http://" + host + "/secret-sentinel"
	want := Failure("")
	if strings.HasPrefix(caseName, "blocked-") || caseName == "mixed" || caseName == "mixed6" || caseName == "additional" || caseName == "reserved-alias" || caseName == "nat-app" {
		want = BlockedTarget
	}
	if strings.HasPrefix(caseName, "dns-") || caseName == "inconsistent" || caseName == "aliases" || caseName == "addresses" || caseName == "records" || caseName == "oversize" || caseName == "total" || caseName == "dname" {
		want = DNSFailed
	}
	if caseName == "dns-timeout" || caseName == "slow" || caseName == "tls-stall" || caseName == "tcp-timeout" {
		want = Timeout
	}
	if caseName == "dns-stage" || caseName == "dns-chain-slow" {
		s.timeout = 30
		s.target = "http://dns-timeout.example.com/"
		if caseName == "dns-chain-slow" {
			s.target = "http://dns-chain-slow.example.com/"
		}
		want = Timeout
	}
	if caseName == "tls-stage" {
		s.timeout = 30
		s.target = "https://tls-stall.example.com/"
		want = Timeout
	}
	if caseName == "tcp-stage" {
		s.timeout = 30
		s.target = "http://tcp-timeout.example.com/"
		want = Timeout
	}
	if caseName == "refuse" {
		want = ConnectFailed
	}
	if caseName == "tls" || caseName == "cname-tls" || caseName == "expired" || caseName == "untrusted" || caseName == "wrong-name" || caseName == "tls-big" || caseName == "tls-stall" {
		s.target = "https://" + host + "/secret-sentinel"
	}
	if caseName == "expired" || caseName == "untrusted" || caseName == "wrong-name" {
		want = TLSFailed
	}
	if caseName == "untrusted" {
		e.roots = x509.NewCertPool()
	}
	if caseName == "wrong-name" {
		s.target = "https://wrong.deep.example.com/secret-sentinel"
	}
	if caseName == "header-big" || caseName == "flood" || caseName == "hints-big" || caseName == "tls-big" {
		want = ResponseLimit
	}
	if caseName == "flood-lf" {
		want = ResponseLimit
	}
	if caseName == "upgrade" || caseName == "malformed" {
		want = ProtocolError
	}
	if strings.HasPrefix(caseName, "redirect-") {
		n := map[string]int64{"redirect-301": 301, "redirect-302": 302, "redirect-307": 307, "redirect-308": 308}[caseName]
		s.expected = n
	}
	if caseName == "status" {
		s.expected = 201
		want = UnexpectedStatus
	}
	if caseName == "t30" {
		s.timeout = 30
		s.target = "http://slow.example.com/secret-sentinel"
		want = Timeout
	}
	if caseName == "capacity" {
		e.gate.records = map[string]gateRecord{}
		for i := 0; i < maxGateRecords; i++ {
			e.gate.records[fmt.Sprint(i)] = gateRecord{retainUntil: time.Now().Add(time.Hour)}
		}
		r, err := e.execute(context.Background(), s)
		if r != nil || !errors.Is(err, ErrResourceLimited) {
			t.Fatal("capacity preflight")
		}
		fmt.Println("PREFLIGHT: resource_limited; no DNS/target attempt")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if caseName == "cancel" {
		s.target = "http://hold.example.com/secret-sentinel"
		s.timeout = 30
		go func() {
			end := time.Now().Add(5 * time.Second)
			for time.Now().Before(end) {
				if fixtureEvent("hold.example.com", "http") {
					cancel()
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
		}()
		want = Canceled
	}
	before, _ := os.ReadDir("/proc/self/fd")
	started := time.Now()
	r, err := e.execute(ctx, s)
	if err != nil || r == nil {
		t.Fatal("unexpected preflight", err)
	}
	got := Failure("")
	if r.FailureCode != nil {
		got = *r.FailureCode
	}
	if got != want {
		t.Fatalf("case %s code=%s want=%s", caseName, got, want)
	}
	if time.Since(started) > time.Duration(s.timeout)*time.Second+time.Second {
		t.Fatal("overall timeout exceeded")
	}
	if caseName == "t30" && time.Since(started) > 6*time.Second {
		t.Fatal("header stage timeout missing")
	}
	if (caseName == "dns-stage" || caseName == "dns-chain-slow") && time.Since(started) > 3*time.Second {
		t.Fatal("shared DNS stage timeout missing")
	}
	if (caseName == "tcp-stage" || caseName == "tls-stage") && time.Since(started) > 4*time.Second {
		t.Fatal("connection stage timeout missing")
	}
	if r.ConfigurationVersion != "7" || r.MonitorID != s.scope.MonitorID || r.Success != (want == "") {
		t.Fatal("result lineage")
	}
	if caseName == "rebinding" {
		s.scope.MonitorID = testScope(2).MonitorID
		r2, err := e.execute(context.Background(), s)
		if err != nil || r2 == nil || r2.FailureCode == nil || *r2.FailureCode != BlockedTarget {
			t.Fatal("second execution did not reject rebind")
		}
	}
	if caseName == "repeat" {
		s.scope.MonitorID = testScope(2).MonitorID
		r2, err := e.execute(context.Background(), s)
		if err != nil || r2 == nil || !r2.Success {
			t.Fatal("new independently checked connection")
		}
	}
	time.Sleep(15 * time.Millisecond)
	after, _ := os.ReadDir("/proc/self/fd")
	if len(after) > len(before)+2 {
		t.Fatal("FD leak")
	}
	b, _ := json.Marshal(r)
	fmt.Println("RESULT:", string(b))
}

func realConcurrency(t *testing.T, e *engine) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	base, _ := os.ReadDir("/proc/self/fd")
	baseGoroutines := runtime.NumGoroutine()
	results := make(chan *Result, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := testSnapshot(i)
			s.scope.WorkspaceID = testScope(100 + i/4).MonitorID
			s.scope.ProjectID = testScope(200 + i/2).MonitorID
			s.target = "http://hold-load.example.com/"
			s.timeout = 30
			r, err := e.execute(ctx, s)
			if err != nil {
				t.Error("load preflight", err)
				return
			}
			results <- r
		}(i)
	}
	eventually(t, func() bool { e.gate.mu.Lock(); defer e.gate.mu.Unlock(); return e.gate.active == 32 })
	if r, err := e.execute(ctx, testSnapshot(99)); r != nil || !errors.Is(err, ErrResourceLimited) {
		t.Fatal("global saturation")
	}
	eventually(t, func() bool { return fixtureEvent("hold-load.example.com", "http") })
	cancel()
	wg.Wait()
	close(results)
	for r := range results {
		if r.FailureCode == nil || *r.FailureCode != Canceled {
			t.Fatal("load cancellation")
		}
	}
	time.Sleep(30 * time.Millisecond)
	runtime.GC()
	after, _ := os.ReadDir("/proc/self/fd")
	if len(after) > len(base)+2 || e.gate.active != 0 {
		t.Fatal("resource recovery")
	}
	if runtime.NumGoroutine() > baseGoroutines+4 {
		t.Fatal("goroutines did not recover")
	}
	fmt.Println("RESOURCE: 32 admitted; excess rejected without queue; cancellation released all active slots/FDs")
}
