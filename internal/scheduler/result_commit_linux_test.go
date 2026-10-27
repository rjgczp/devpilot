//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestResultCommitIdentityImmutableAndRedacted(t *testing.T) {
	l, r := resultExample()
	v, e := NetworkResult(l, r)
	if e != nil {
		t.Fatal(e)
	}
	c, e := NewResultCommit(v)
	if e != nil {
		t.Fatal(e)
	}
	copy := c
	if len(c.id) != 36 || copy.id != c.id || copy.value != c.value {
		t.Fatal("unstable identity")
	}
	*r.HTTPStatus = 599
	if *c.value.status != 200 {
		t.Fatal("source mutated frozen payload")
	}
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		if text := fmt.Sprintf(f, c); strings.Contains(text, c.id) || strings.Contains(text, l.RunID) {
			t.Fatal("identity leaked")
		}
	}
	ctrl, e := ControlResult(l, "permit_denied", "commit_unknown")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = NewResultCommit(ctrl); e != ErrResult {
		t.Fatal("control became network result")
	}
	if _, e = NewResultCommit(MinimalResult{}); e != ErrResult {
		t.Fatal("empty result accepted")
	}
}

// A deprecated Go call fails before opening a DB transaction, without consuming
// the observation needed by the fixed-ID path. The DB refusal is tested in PG18.
func TestLegacyNetworkWriterRefusesBeforeDatabase(t *testing.T) {
	l, r := resultExample()
	v, err := NetworkResult(l, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = (Store{}).PersistMinimalResult(context.Background(), v); err != ErrLegacyNetworkResult {
		t.Fatal(err)
	}
	if _, err = NewResultCommit(v); err != nil {
		t.Fatal("refusal consumed observation", err)
	}
	var writer ResultWriter = Store{}
	if err = writer.PersistControlResult(context.Background(), ControlOutcome{}); err != ErrResult {
		t.Fatal(err)
	}
}
