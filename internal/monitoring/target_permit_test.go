package monitoring

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestTargetPermitSoleDialGate(t *testing.T) {
	for _, kind := range []string{"missing", "denied", "disconnect", "unknown-commit", "supervisor-missing", "expired", "cancel", "late-allow", "allow"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			c := targetCandidate{scope: testSnapshot(1).scope, version: 1, host: "a.example.com", scheme: "https", endpoint: netip.MustParseAddrPort("93.184.216.34:443"), deadline: time.Now().Add(time.Second)}
			calls := 0
			var hook targetPermitHook
			if kind != "missing" {
				hook = func(ctx context.Context, got targetCandidate) error {
					if got != c {
						t.Fatal("mutated original origin/literal")
					}
					switch kind {
					case "allow":
						return nil
					case "cancel":
						cancel()
						return nil
					case "late-allow":
						<-ctx.Done()
						return nil
					default:
						return errors.New("private-network-secret-SENTINEL")
					}
				}
			}
			if kind == "expired" {
				c.deadline = time.Now().Add(-time.Second)
			}
			conn, err := permittedDial(ctx, c, hook, func() (net.Conn, error) { calls++; return nil, nil })
			if kind == "allow" {
				if err != nil || calls != 1 {
					t.Fatal("offline allow gate")
				}
			} else if !errors.Is(err, ErrTargetPermit) || calls != 0 || conn != nil {
				t.Fatal("denial reached dial", kind, calls)
			}
		})
	}
	e, err := newEngine(netip.MustParseAddr("127.0.0.53"), nil)
	if err != nil || e.targetPermit != nil {
		t.Fatal("default is not deny")
	}
}
