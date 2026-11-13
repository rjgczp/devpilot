//go:build privateprobe && linux

package monitoring

import (
	"os"
	"testing"
)

func TestSupervisorLifetimeIsReadOnlyAndFailsClosed(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if !SupervisorAlive(r) || SupervisorAlive(w) || SupervisorAlive(nil) {
		t.Fatal("invalid lifetime authority")
	}
	w.Close()
	if SupervisorAlive(r) {
		t.Fatal("lost Supervisor accepted")
	}
	f, e := os.CreateTemp(t.TempDir(), "not-pipe")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if SupervisorAlive(f) {
		t.Fatal("file faked liveness")
	}
}
