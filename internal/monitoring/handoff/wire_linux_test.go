//go:build schedulerfixture && linux

package handoff

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture() Envelope {
	n := time.Now().UTC()
	return Envelope{Lineage: Lineage{WorkspaceID: "01900000-0000-7000-8000-000000000001", ProjectID: "01900000-0000-7000-8000-000000000002", MonitorID: "01900000-0000-7000-8000-000000000003", RunID: "01900000-0000-7000-8000-000000000004", ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "3", FencingToken: "4", NamespaceIncarnation: "01900000-0000-7000-8000-000000000005", SupervisorIncarnation: "01900000-0000-7000-8000-000000000006"}, Target: []byte("https://fixture.example.com/private"), StartedAt: n, PermitDeadline: n.Add(35 * time.Second), TimeoutSeconds: 10, ExpectedStatus: 200, IntervalSeconds: 60}
}
func inputPipe(t *testing.T, data []byte) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(data); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() { r.Close() })
	return r
}
func TestAnonymousInputStrictFraming(t *testing.T) {
	for _, mode := range []string{"success", "truncated", "extra", "padding", "duplicate-key", "unknown-key", "invalid-unicode", "bad-scope", "bad-generation", "expired", "future-start"} {
		t.Run(mode, func(t *testing.T) {
			e := fixture()
			if mode == "bad-scope" {
				e.ProjectID = "bad"
			}
			if mode == "bad-generation" {
				e.ActivationGeneration = "01"
			}
			if mode == "expired" {
				e.PermitDeadline = time.Now().Add(-time.Second)
			}
			if mode == "future-start" {
				e.StartedAt = time.Now().Add(time.Hour)
				e.PermitDeadline = e.StartedAt.Add(35 * time.Second)
			}
			frame, r, err := Encode(e)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "duplicate-key" || mode == "unknown-key" || mode == "invalid-unicode" {
				n := int(binary.BigEndian.Uint32(frame[8:12]))
				body := string(frame[12 : 12+n])
				switch mode {
				case "duplicate-key":
					body = strings.Replace(body, `"run_id":`, `"run_id":"01900000-0000-7000-8000-000000000009","run_id":`, 1)
				case "unknown-key":
					body = `{"unexpected":true,` + body[1:]
				case "invalid-unicode":
					body = strings.Replace(body, `"fencing_token":"4"`, `"fencing_token":"\ud800"`, 1)
				}
				frame = make([]byte, FrameSize)
				copy(frame, magic)
				binary.BigEndian.PutUint32(frame[8:12], uint32(len(body)))
				copy(frame[12:], body)
				r.Digest = sha256.Sum256(frame)
			}
			switch mode {
			case "truncated":
				frame = frame[:50]
			case "extra":
				frame = append(frame, 1)
			case "padding":
				frame[FrameSize-1] = 1
				r.Digest = sha256.Sum256(frame)
			}
			receiver := NewReceiver(r)
			got, err := receiver.Receive(inputPipe(t, frame))
			if mode == "success" {
				if err != nil || !bytes.Equal(got.Target, e.Target) {
					t.Fatal("valid fixed frame", err)
				}
			} else if err == nil {
				t.Fatal("invalid frame accepted", mode)
			}
			if _, err = receiver.Receive(inputPipe(t, frame)); err == nil {
				t.Fatal("failure restored receiving capability")
			}
		})
	}
}
func TestAnonymousReceiverRaceSingleConsumption(t *testing.T) {
	frame, r, _ := Encode(fixture())
	receiver := NewReceiver(r)
	f := inputPipe(t, frame)
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := receiver.Receive(f); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("receiving race lost or duplicated capability", successes.Load())
	}
}
