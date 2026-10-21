//go:build schedulerfixture && linux

package scheduler

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

func TestTargetDirectoryCanonicalHMACAndIsolation(t *testing.T) {
	a := netip.MustParseAddr("192.0.2.10")
	key := []byte(strings.Repeat("SECRET_SENTINEL_", 3))
	d, err := NewTargetDirectory(map[int64][]byte{1: key}, []FixtureTarget{{URL: "https://A.fixture.test/registered", Addresses: []netip.Addr{a}}, {URL: "https://alias.fixture.test/", Addresses: []netip.Addr{a}}})
	if err != nil {
		t.Fatal(err)
	}
	h := Hint{WorkspaceID: "00000000-0000-0000-0000-000000000001", ProjectID: "00000000-0000-0000-0000-000000000002", MonitorID: "00000000-0000-0000-0000-000000000003", ConfigurationVersion: "1"}
	e, err := d.Bind(h, "https://a.fixture.test:443/PRIVATE_SENTINEL?x=1", a)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := d.Bind(h, "https://A.fixture.test/another", a)
	alias, _ := d.Bind(h, "https://alias.fixture.test/", a)
	if e.Keys[0] != other.Keys[0] || e.Keys[0].OriginKey == alias.Keys[0].OriginKey || e.Keys[0].IPKey != alias.Keys[0].IPKey || e.Keys[0].OriginKey == e.Keys[0].IPKey {
		t.Fatal("canonical/domain separation")
	}
	// Caller mutation cannot rotate an immutable directory behind active work.
	key[0] ^= 0xff
	unchanged, _ := d.Bind(h, "https://a.fixture.test/", a)
	if unchanged.Keys[0] != e.Keys[0] {
		t.Fatal("shared secret storage")
	}
	for _, raw := range []string{"https://evil.test/", "https://user@a.fixture.test/", "ftp://a.fixture.test/", "https://a.fixture.test:444/"} {
		if _, err = d.Bind(h, raw, a); err == nil {
			t.Fatal("unregistered target")
		}
	}
	for _, ip := range []netip.Addr{netip.MustParseAddr("192.0.2.11"), netip.MustParseAddr("::ffff:192.0.2.10"), netip.Addr{}} {
		if _, err = d.Bind(h, "https://a.fixture.test/", ip); err == nil {
			t.Fatal("address replaced")
		}
	}
	raw, _ := json.Marshal(e)
	directory, _ := json.Marshal(d)
	formatted := fmt.Sprintf("%v %#v %+v", d, d, d)
	for _, s := range []string{"fixture.test", "192.0.2.10", "PRIVATE_SENTINEL", "SECRET_SENTINEL"} {
		if strings.Contains(string(raw)+string(directory)+formatted, s) {
			t.Fatal("raw material escaped redaction")
		}
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				x, err := d.Bind(h, "https://a.fixture.test/", a)
				if err != nil || x.Keys[0] != e.Keys[0] {
					t.Error("concurrent directory mutation")
				}
			}
		}()
	}
	wg.Wait()
}
func TestTargetDirectoryRotationDualDigests(t *testing.T) {
	ip := netip.MustParseAddr("2001:db8::10")
	fixtures := []FixtureTarget{{URL: "http://fixture.test", Addresses: []netip.Addr{ip}}}
	keys := map[int64][]byte{1: []byte(strings.Repeat("x", 32)), 2: []byte(strings.Repeat("y", 32))}
	d, err := NewTargetDirectory(keys, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	h := Hint{WorkspaceID: "00000000-0000-0000-0000-000000000001", ProjectID: "00000000-0000-0000-0000-000000000002", MonitorID: "00000000-0000-0000-0000-000000000003", ConfigurationVersion: "1"}
	e, err := d.Bind(h, "http://fixture.test:80/", ip)
	if err != nil || len(e.Keys) != 2 || e.Keys[0].Generation != "1" || e.Keys[1].Generation != "2" || e.Keys[0].OriginKey == e.Keys[1].OriginKey {
		t.Fatal("retained generations", err)
	}
	for _, bad := range []map[int64][]byte{{}, {0: keys[1]}, {1: []byte("short")}, {1: keys[1], 3: keys[2]}} {
		if _, err = NewTargetDirectory(bad, fixtures); err == nil {
			t.Fatal("unbounded or invalid keyring")
		}
	}
}

func TestBudgetCapacityErrorDoesNotWidenConstraintMapping(t *testing.T) {
	if databaseError(&pgconn.PgError{Code: "23514", Message: "retention_capacity"}) != ErrCapacity {
		t.Fatal("missing fixed capacity mapping")
	}
	for _, message := range []string{"budget identity clock or cooling is immutable", "other constraint"} {
		if databaseError(&pgconn.PgError{Code: "23514", Message: message}) != ErrUnavailable {
			t.Fatal("unrelated safety constraint hidden as quota")
		}
	}
}
