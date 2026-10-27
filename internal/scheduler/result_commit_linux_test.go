//go:build schedulerfixture && linux

package scheduler

import (
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
