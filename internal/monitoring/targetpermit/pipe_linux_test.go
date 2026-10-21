//go:build schedulerfixture && linux

package targetpermit

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

func testRequest() Request {
	const id = "10000000-0000-0000-0000-000000000001"
	return Request{Lineage: handoff.Lineage{WorkspaceID: id, ProjectID: id, MonitorID: id, RunID: id, ConfigurationVersion: "1", ActivationGeneration: "1", SchedulerEpoch: "1", FencingToken: "1", NamespaceIncarnation: id, SupervisorIncarnation: id}, Host: "a.fixture.example.com", Scheme: "https", IP: netip.MustParseAddr("93.184.216.34"), Port: 443, Deadline: time.Now().UTC().Add(time.Second)}
}
func pipes(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return r, w
}
func TestTargetPipeOneAttempt(t *testing.T) {
	for _, kind := range []string{"allow", "denied", "disconnect", "timeout", "wrong-run", "wrong-ip", "wrong-host", "wrong-deadline", "wrong-fence", "missing-supervisor"} {
		t.Run(kind, func(t *testing.T) {
			rr, rw := pipes(t)
			ar, aw := pipes(t)
			client := NewClient(rw, ar)
			r := testRequest()
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			ch := make(chan error, 1)
			go func() {
				if kind == "disconnect" {
					rr.Close()
					aw.Close()
					ch <- ErrDenied
					return
				}
				ch <- (&Server{}).Serve(ctx, rr, aw, func(ctx context.Context, request Request) (Confirmation, error) {
					a := Confirmation{Request: request, PermitID: request.RunID, Deadline: request.Deadline}
					switch kind {
					case "denied", "missing-supervisor":
						return Confirmation{}, errors.New("private-secret")
					case "timeout":
						<-ctx.Done()
						return a, nil
					case "wrong-run":
						a.Request.RunID = "20000000-0000-0000-0000-000000000001"
					case "wrong-ip":
						a.Request.IP = netip.MustParseAddr("93.184.216.35")
					case "wrong-host":
						a.Request.Host = "other.example.com"
					case "wrong-deadline":
						a.Deadline = a.Deadline.Add(time.Second)
					case "wrong-fence":
						a.Request.FencingToken = "2"
					}
					return a, nil
				})
			}()
			err := client.Acquire(ctx, r)
			serverErr := <-ch
			if kind == "allow" {
				if err != nil || serverErr != nil {
					t.Fatal("allow protocol", err, serverErr)
				}
			} else if !errors.Is(err, ErrDenied) {
				t.Fatal("uncertain reply allowed", kind)
			}
			if client.Acquire(ctx, r) == nil {
				t.Fatal("second attempt restored capability")
			}
		})
	}
}
func TestTargetPipeConcurrentSingleConsumer(t *testing.T) {
	rr, rw := pipes(t)
	ar, aw := pipes(t)
	client := NewClient(rw, ar)
	r := testRequest()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	var received atomic.Int32
	go func() {
		done <- (&Server{}).Serve(ctx, rr, aw, func(_ context.Context, r Request) (Confirmation, error) {
			received.Add(1)
			return Confirmation{Request: r, PermitID: r.RunID, Deadline: r.Deadline}, nil
		})
	}()
	var wg sync.WaitGroup
	var success atomic.Int32
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if client.Acquire(ctx, r) == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if err := <-done; err != nil || success.Load() != 1 || received.Load() != 1 {
		t.Fatal("duplicate pipe consumption", err, success.Load(), received.Load())
	}
}
func TestTargetPipeMalformed(t *testing.T) {
	for _, kind := range []string{"truncated", "oversize", "padding", "unknown-key", "duplicate-key", "invalid-unicode", "expired", "cross-run"} {
		t.Run(kind, func(t *testing.T) {
			r := testRequest()
			b, _ := frame(r)
			switch kind {
			case "truncated":
				b = b[:20]
			case "oversize":
				b = append(b, 1)
			case "padding":
				b[len(b)-1] = 1
			case "unknown-key":
				b, _ = frame(map[string]string{"extra": "x"})
			case "duplicate-key":
				b = make([]byte, FrameSize)
				copy(b, "DVTP")
				p := []byte(`{"host":"a","host":"b"}`)
				binary.BigEndian.PutUint32(b[4:8], uint32(len(p)))
				copy(b[8:], p)
			case "invalid-unicode":
				b = make([]byte, FrameSize)
				copy(b, "DVTP")
				p := []byte(`{"host":"\ud800"}`)
				binary.BigEndian.PutUint32(b[4:8], uint32(len(p)))
				copy(b[8:], p)
			case "expired":
				r.Deadline = time.Now().Add(-time.Second)
				b, _ = frame(r)
			case "cross-run":
				r.RunID = ""
				b, _ = frame(r)
			}
			rr, rw := pipes(t)
			ar, aw := pipes(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() { _, _ = rw.Write(b); rw.Close() }()
			calls := 0
			err := (&Server{}).Serve(ctx, rr, aw, func(context.Context, Request) (Confirmation, error) { calls++; return Confirmation{}, nil })
			ar.Close()
			if err == nil || calls != 0 {
				t.Fatal("malformed request reached controller", kind)
			}
		})
	}
}

func TestTargetPipeRejectsUntrustedReply(t *testing.T) {
	for _, kind := range []string{"cross-run", "cross-ip", "cross-host", "cross-epoch", "cross-namespace", "expired", "extra-byte", "truncated", "invalid-id"} {
		t.Run(kind, func(t *testing.T) {
			rr, rw := pipes(t)
			ar, aw := pipes(t)
			r := testRequest()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer rr.Close()
				defer aw.Close()
				var received Request
				if read(context.Background(), rr, r.Deadline, &received) != nil {
					return
				}
				a := Confirmation{Request: received, PermitID: received.RunID, Deadline: received.Deadline}
				switch kind {
				case "cross-run":
					a.Request.RunID = "20000000-0000-0000-0000-000000000001"
				case "cross-ip":
					a.Request.IP = netip.MustParseAddr("192.0.2.10")
				case "cross-host":
					a.Request.Host = "other.example.com"
				case "cross-epoch":
					a.Request.SchedulerEpoch = "999"
				case "cross-namespace":
					a.Request.NamespaceIncarnation = "20000000-0000-0000-0000-000000000001"
				case "expired":
					a.Deadline = time.Now().Add(-time.Second)
				case "invalid-id":
					a.PermitID = "not-a-permit"
				}
				b, _ := frame(a)
				if kind == "extra-byte" {
					b = append(b, 1)
				}
				if kind == "truncated" {
					b = b[:100]
				}
				_, _ = aw.Write(b)
			}()
			if NewClient(rw, ar).Acquire(context.Background(), r) == nil {
				t.Fatal("untrusted reply allowed", kind)
			}
			<-done
		})
	}
}
