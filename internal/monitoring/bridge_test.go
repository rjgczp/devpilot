package monitoring

// Only test binaries can serialize a snapshot to the isolated child via stdin.
// Production Snapshot JSON stays {}, with no arbitrary-network injection API.
type fixtureWire struct {
	Scope                                Scope
	Name, Target, Method                 string
	Interval, Timeout, Expected, Version int64
	Want                                 Failure
}

func wireSnapshot(s Snapshot, want Failure) fixtureWire {
	return fixtureWire{s.scope, s.name, s.target, s.method, s.interval, s.timeout, s.expected, s.version, want}
}
func (w fixtureWire) snapshot() Snapshot {
	return Snapshot{scope: w.Scope, name: w.Name, target: w.Target, method: w.Method, interval: w.Interval, timeout: w.Timeout, expected: w.Expected, version: w.Version}
}
