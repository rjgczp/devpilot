//go:build schedulerfixture && linux

// Package handoff is a bounded anonymous-pipe protocol, with no execution entry.
package handoff

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rjgczp/devpilot/internal/credentials"
	"golang.org/x/sys/unix"
)

const FrameSize = 4096
const ReceiptSize = 1024

var ErrInput = errors.New("invalid or consumed execution input")
var magic = []byte("DVPIN001")

type Lineage struct {
	WorkspaceID           string `json:"workspace_id"`
	ProjectID             string `json:"project_id"`
	MonitorID             string `json:"monitor_id"`
	RunID                 string `json:"run_id"`
	ConfigurationVersion  string `json:"configuration_version"`
	ActivationGeneration  string `json:"activation_generation"`
	SchedulerEpoch        string `json:"scheduler_epoch"`
	FencingToken          string `json:"fencing_token"`
	NamespaceIncarnation  string `json:"namespace_incarnation"`
	SupervisorIncarnation string `json:"supervisor_incarnation"`
}
type Envelope struct {
	Lineage
	StartedAt       time.Time `json:"started_at"`
	PermitDeadline  time.Time `json:"permit_deadline"`
	Target          []byte    `json:"target"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	ExpectedStatus  int       `json:"expected_status"`
	IntervalSeconds int       `json:"interval_seconds"`
}

func (Envelope) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<private execution envelope>") }

type Receipt struct {
	Lineage
	Digest         [32]byte  `json:"digest"`
	PermitDeadline time.Time `json:"permit_deadline"`
}

func uuid(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range []byte(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (l Lineage) Valid() bool {
	for _, s := range []string{l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.NamespaceIncarnation, l.SupervisorIncarnation} {
		if !uuid(s) {
			return false
		}
	}
	for _, s := range []string{l.ConfigurationVersion, l.ActivationGeneration, l.SchedulerEpoch, l.FencingToken} {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != s {
			return false
		}
	}
	return true
}
func fixed(v any, n int) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil || len(b) > n-12 {
		return nil, ErrInput
	}
	frame := make([]byte, n)
	copy(frame, magic)
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(b)))
	copy(frame[12:], b)
	return frame, nil
}
func payload(frame []byte, size int) ([]byte, error) {
	if len(frame) != size || !bytes.Equal(frame[:8], magic) {
		return nil, ErrInput
	}
	n := int(binary.BigEndian.Uint32(frame[8:12]))
	if n < 2 || n > size-12 {
		return nil, ErrInput
	}
	for _, b := range frame[12+n:] {
		if b != 0 {
			return nil, ErrInput
		}
	}
	b := frame[12 : 12+n]
	if !credentials.ValidJSON(b) {
		return nil, ErrInput
	}
	return b, nil
}
func decode(frame []byte, size int, v any) error {
	b, err := payload(frame, size)
	if err != nil {
		return ErrInput
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInput
	}
	canonical, err := json.Marshal(v)
	if err != nil || !bytes.Equal(canonical, b) {
		return ErrInput
	}
	return nil
}

// Encode never emits a URL through a generic diagnostic projection.
func Encode(e Envelope) ([]byte, Receipt, error) {
	frame, err := fixed(e, FrameSize)
	if err != nil {
		return nil, Receipt{}, ErrInput
	}
	return frame, Receipt{Lineage: e.Lineage, Digest: sha256.Sum256(frame), PermitDeadline: e.PermitDeadline}, nil
}
func ReceiptFrame(r Receipt) ([]byte, error) { return fixed(r, ReceiptSize) }
func anonymous(f *os.File) bool {
	if f == nil {
		return false
	}
	var s unix.Stat_t
	if unix.Fstat(int(f.Fd()), &s) != nil || s.Mode&unix.S_IFMT != unix.S_IFIFO {
		return false
	}
	link, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	return err == nil && strings.HasPrefix(link, "pipe:[")
}

// InheritedPipe makes an inherited anonymous reader pollable, with a fixed
// receive budget. Named FIFOs, regular files and sockets are never accepted.
func InheritedPipe(fd int) (*os.File, error) {
	if fd < 3 || fd > 4 || unix.SetNonblock(fd, true) != nil {
		return nil, ErrInput
	}
	f := os.NewFile(uintptr(fd), "private-input")
	if !anonymous(f) {
		if f != nil {
			_ = f.Close()
		}
		return nil, ErrInput
	}
	return f, nil
}
func readFixed(f *os.File, n int, deadline time.Time) ([]byte, error) {
	if !anonymous(f) {
		return nil, ErrInput
	}
	defer f.Close()
	limit := time.Now().Add(2 * time.Second)
	if !deadline.IsZero() && deadline.Before(limit) {
		limit = deadline
	}
	if !limit.After(time.Now()) || f.SetReadDeadline(limit) != nil {
		return nil, ErrInput
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(f, b); err != nil {
		return nil, ErrInput
	}
	var extra [1]byte
	if k, err := f.Read(extra[:]); k != 0 || err != io.EOF {
		return nil, ErrInput
	}
	return b, nil
}
func ReadReceipt(f *os.File) (Receipt, error) {
	b, err := readFixed(f, ReceiptSize, time.Time{})
	if err != nil {
		return Receipt{}, ErrInput
	}
	var r Receipt
	if decode(b, ReceiptSize, &r) != nil || !r.Lineage.Valid() || !r.PermitDeadline.After(time.Now()) {
		return Receipt{}, ErrInput
	}
	return r, nil
}

type Receiver struct {
	expected Receipt
	consumed atomic.Bool
}

func NewReceiver(r Receipt) *Receiver { return &Receiver{expected: r} }

// Every invocation consumes the receiving capability, including malformed,
// partial, expired and failed inputs. There is no reconnect/restore operation.
func (r *Receiver) Receive(f *os.File) (Envelope, error) {
	if r == nil || !r.consumed.CompareAndSwap(false, true) {
		return Envelope{}, ErrInput
	}
	b, err := readFixed(f, FrameSize, r.expected.PermitDeadline)
	if err != nil {
		return Envelope{}, ErrInput
	}
	digest := sha256.Sum256(b)
	if subtle.ConstantTimeCompare(digest[:], r.expected.Digest[:]) != 1 {
		return Envelope{}, ErrInput
	}
	var e Envelope
	if decode(b, FrameSize, &e) != nil || e.Lineage != r.expected.Lineage || !e.Lineage.Valid() || !e.PermitDeadline.Equal(r.expected.PermitDeadline) || !e.PermitDeadline.After(time.Now()) || len(e.Target) < 1 || len(e.Target) > 2048 || e.TimeoutSeconds < 1 || e.TimeoutSeconds > 30 || e.ExpectedStatus < 200 || e.ExpectedStatus > 599 || e.StartedAt.IsZero() || e.StartedAt.After(time.Now().Add(2*time.Second)) || e.PermitDeadline.Before(e.StartedAt.Add(time.Duration(e.TimeoutSeconds)*time.Second)) || e.PermitDeadline.After(e.StartedAt.Add(35*time.Second)) {
		return Envelope{}, ErrInput
	}
	return e, nil
}
