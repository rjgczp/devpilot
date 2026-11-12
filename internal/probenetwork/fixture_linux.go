//go:build schedulerfixture && linux

package probenetwork

import "context"

func BootFixture(ctx context.Context, c Config) (*Policy, error) {
	if c.Target.IsValid() || len(c.Denied) != 0 {
		return nil, ErrDenied
	}
	return boot(ctx, c, false)
}
