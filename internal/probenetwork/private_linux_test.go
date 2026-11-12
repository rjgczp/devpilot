//go:build schedulerfixture && linux

package probenetwork

import (
	"context"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"net/netip"
	"testing"
	"time"
)

func TestPrivateConfigBindsOneHTTPSPublicTarget(t *testing.T) {
	c := testConfig()
	c.Target = netip.MustParseAddr("93.184.216.34")
	c.Denied = []netip.Prefix{netip.MustParsePrefix("11.0.0.0/8"), netip.MustParsePrefix("2600:dead::/32")}
	if !validPrivateConfig(c, time.Now()) {
		t.Fatal("valid private binding")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "11.0.0.1", "192.0.2.53", "198.19.64.2", "::1", "fd00::1", "fe80::1", "64:ff9b::5db8:d822", "::ffff:93.184.216.34", "2600:dead::1"} {
		x := c
		x.Target = netip.MustParseAddr(ip)
		if validPrivateConfig(x, time.Now()) {
			t.Fatal("blocked private/metadata/host/control address accepted", ip)
		}
	}
	for _, ip := range []string{"93.184.216.34", "2001:4860:ffff::34"} {
		x := c
		x.Target = netip.MustParseAddr(ip)
		if !validPrivateConfig(x, time.Now()) {
			t.Fatal("family rejected")
		}
	}
	for _, change := range []func(*Config){func(c *Config) { c.Denied = nil }, func(c *Config) { c.Host = "bad/path" }, func(c *Config) { c.HardDeadline = time.Now().Add(time.Minute) }} {
		x := c
		change(&x)
		if validPrivateConfig(x, time.Now()) {
			t.Fatal("unsafe config")
		}
	}
	for _, scheme := range []string{"http", "https"} {
		p := &Policy{c: c, private: true, hardMono: time.Now().Add(time.Second)}
		r := targetpermit.Request{Lineage: c.Lineage, Host: c.Host, Scheme: scheme, IP: netip.MustParseAddr("8.8.8.8"), Port: 443, Deadline: time.Now().Add(time.Second)}
		if p.allowedTarget(r) {
			t.Fatal("other target accepted")
		}
		if _, e := p.Install(context.Background(), Install{Request: r}); e == nil {
			t.Fatal("uncommitted/mismatched install")
		}
	}
}
