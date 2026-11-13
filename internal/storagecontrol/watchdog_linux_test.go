//go:build linux

package storagecontrol

import (
	"context"
	"encoding/binary"
	"golang.org/x/sys/unix"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestGuardSustainedFreshPulses(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid := unix.Gettid()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			_ = unix.Tgkill(os.Getpid(), tid, unix.SIGURG)
		}
	}()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	defer w.Close()
	g := NewGuard(r)
	end := time.Now().Add(2 * time.Second)
	for time.Now().Before(end) {
		b, e := pulse()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(b); e != nil {
			t.Fatal(e)
		}
		if !g.Alive() {
			t.Fatal("fresh admission rejected", g.Rejection())
		}
		runtime.Gosched()
	}
}

type denyPublisher struct{ deny chan bool }

func (p denyPublisher) Begin(context.Context) (Challenge, error) { panic("watchdog cannot sample") }
func (p denyPublisher) Publish(context.Context, Challenge, any) error {
	panic("watchdog cannot sample")
}
func (p denyPublisher) Unavailable(context.Context) error { p.deny <- true; return ErrReport }
func TestMonotonicGuardRejectsMissingStaleAndEOF(t *testing.T) {
	r, w, _ := os.Pipe()
	defer r.Close()
	g := NewGuard(r)
	if g.Alive() {
		t.Fatal("missing pulse")
	}
	b, _ := pulse()
	w.Write(b)
	if !g.Alive() {
		t.Fatal("fresh pulse rejected")
	}
	w.Close()
	if g.Alive() {
		t.Fatal("EOF retained cached availability")
	}
	old, _ := bootNow()
	stale := make([]byte, 8)
	binary.BigEndian.PutUint64(stale, uint64(old-int64(maxPulseAge)-1))
	if fresh(stale) {
		t.Fatal("stale monotonic pulse accepted")
	}
	if fresh(make([]byte, 8)) {
		t.Fatal("zero pulse")
	}
}
func TestIndependentWatchdogClosesAdmissionBeforeSQLFailure(t *testing.T) {
	r, w, _ := os.Pipe()
	gr, gw, _ := os.Pipe()
	defer r.Close()
	defer gr.Close()
	// os.Pipe is pollable. The real CLI additionally validates inherited access.
	p := denyPublisher{make(chan bool, 1)}
	done := make(chan error, 1)
	go func() { done <- Watch(context.Background(), p, r, []*os.File{gw}) }()
	b, _ := pulse()
	w.Write(b)
	buf := make([]byte, 8)
	gr.SetReadDeadline(time.Now().Add(time.Second))
	if _, e := gr.Read(buf); e != nil || !fresh(buf) {
		t.Fatal(e)
	}
	w.Close()
	if <-done == nil {
		t.Fatal("lost sampler accepted")
	}
	<-p.deny
	fds := []unix.PollFd{{Fd: int32(gr.Fd()), Events: unix.POLLHUP}}
	unix.Poll(fds, 1000)
	if fds[0].Revents&unix.POLLHUP == 0 {
		t.Fatal("DB failure left local permission open")
	}
}
