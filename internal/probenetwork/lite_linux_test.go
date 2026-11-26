//go:build (schedulerfixture || privateprobe) && linux

package probenetwork

import (
	"context"
	"net/netip"
	"testing"
)

func TestLegacyPrivateEntryRejectsLiteBeforeAnyKernelCommand(t *testing.T) {
	c := Config{Lite: &LiteProfile{Resolver: netip.MustParseAddr("1.1.1.1")}}
	if _, e := BootPrivate(context.Background(), c); e != ErrDenied {
		t.Fatal("legacy entry accepted Lite")
	}
	if _, e := BootLite(context.Background(), Config{}); e != ErrDenied {
		t.Fatal("missing Lite configuration accepted")
	}
	if resolver4(Config{}) != "192.0.2.53" || resolver4(c) != "1.1.1.1" {
		t.Fatal("resolver profiles mixed")
	}
}
