//go:build schedulerfixture && linux

// Package targetpermit carries one bounded, credential-free request/reply over
// two trusted anonymous pipes. It installs no rules and has no networking entry.
package targetpermit

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"golang.org/x/sys/unix"
)

const FrameSize = 2048

var ErrDenied = errors.New("target_permit_unavailable")

type Request struct {
	handoff.Lineage
	Host     string     `json:"host"`
	Scheme   string     `json:"scheme"`
	IP       netip.Addr `json:"ip"`
	Port     uint16     `json:"port"`
	Deadline time.Time  `json:"deadline"`
}

func (Request) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<private target request>") }
func (r Request) Valid() bool {
	return r.Lineage.Valid() && len(r.Host) > 0 && len(r.Host) <= 253 && strings.ToLower(r.Host) == r.Host && !strings.ContainsAny(r.Host, "/:@%\\\x00\r\n\t ") && r.IP.IsValid() && !r.IP.Is4In6() && r.IP.Zone() == "" && ((r.Scheme == "http" && r.Port == 80) || (r.Scheme == "https" && r.Port == 443)) && r.Deadline.After(time.Now())
}

// Confirmation is a trusted Supervisor protocol reply, NOT a signature or
// evidence that rules actually exist. The independent fixture installer verifies them.
// It must arrive on the separate trusted management channel, never from Probe.
type Confirmation struct {
	Request  Request   `json:"request"`
	PermitID string    `json:"permit_id"`
	Deadline time.Time `json:"deadline"`
}

func (Confirmation) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<private target confirmation>")
}
func (a Confirmation) Matches(r Request) bool {
	return r.Valid() && a.Request == r && validUUID(a.PermitID) && a.Deadline.Equal(r.Deadline) && a.Deadline.After(time.Now())
}
func validUUID(s string) bool {
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
func frame(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil || len(b) > FrameSize-8 {
		return nil, ErrDenied
	}
	out := make([]byte, FrameSize)
	copy(out, "DVTP")
	binary.BigEndian.PutUint32(out[4:8], uint32(len(b)))
	copy(out[8:], b)
	return out, nil
}
func anonymous(f *os.File) bool {
	if f == nil {
		return false
	}
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFIFO {
		return false
	}
	p, e := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	return e == nil && strings.HasPrefix(p, "pipe:[")
}
func deadline(ctx context.Context, hard time.Time) time.Time {
	d := time.Now().Add(2 * time.Second)
	if !hard.IsZero() && hard.Before(d) {
		d = hard
	}
	if c, ok := ctx.Deadline(); ok && c.Before(d) {
		d = c
	}
	return d
}
func read(ctx context.Context, f *os.File, hard time.Time, v any) error {
	if !anonymous(f) || ctx.Err() != nil || f.SetReadDeadline(deadline(ctx, hard)) != nil {
		return ErrDenied
	}
	stop := context.AfterFunc(ctx, func() { _ = f.SetReadDeadline(time.Now()) })
	defer stop()
	b := make([]byte, FrameSize)
	if _, e := io.ReadFull(f, b); e != nil {
		return ErrDenied
	}
	var extra [1]byte
	if n, e := f.Read(extra[:]); n != 0 || e != io.EOF {
		return ErrDenied
	}
	if string(b[:4]) != "DVTP" {
		return ErrDenied
	}
	n := int(binary.BigEndian.Uint32(b[4:8]))
	if n < 2 || n > FrameSize-8 {
		return ErrDenied
	}
	for _, c := range b[8+n:] {
		if c != 0 {
			return ErrDenied
		}
	}
	payload := b[8 : 8+n]
	if !credentials.ValidJSON(payload) {
		return ErrDenied
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil {
		return ErrDenied
	}
	canonical, e := json.Marshal(v)
	if e != nil || !bytes.Equal(canonical, payload) || ctx.Err() != nil {
		return ErrDenied
	}
	return nil
}
func write(ctx context.Context, f *os.File, hard time.Time, v any) error {
	if !anonymous(f) || ctx.Err() != nil || f.SetWriteDeadline(deadline(ctx, hard)) != nil {
		return ErrDenied
	}
	stop := context.AfterFunc(ctx, func() { _ = f.SetWriteDeadline(time.Now()) })
	defer stop()
	b, e := frame(v)
	if e != nil {
		return ErrDenied
	}
	n, e := f.Write(b)
	if e != nil || n != len(b) || ctx.Err() != nil {
		return ErrDenied
	}
	return nil
}

type Client struct {
	request, reply *os.File
	used           atomic.Bool
}

func NewClient(requestWriter, replyReader *os.File) *Client {
	return &Client{request: requestWriter, reply: replyReader}
}
func (c *Client) Acquire(ctx context.Context, r Request) error {
	if c == nil || !c.used.CompareAndSwap(false, true) {
		return ErrDenied
	}
	if c.request != nil {
		defer c.request.Close()
	}
	if c.reply != nil {
		defer c.reply.Close()
	}
	r.Deadline = r.Deadline.UTC()
	if !r.Valid() || write(ctx, c.request, r.Deadline, r) != nil {
		return ErrDenied
	}
	_ = c.request.Close()
	var a Confirmation
	if read(ctx, c.reply, r.Deadline, &a) != nil || !a.Matches(r) {
		return ErrDenied
	}
	return nil
}

type Server struct{ used atomic.Bool }

// Serve closes both descriptors on all failures. EOF is denial. There is no
// retry/reconnect operation, raw failure reason, DB material or allow cache.
func (s *Server) Serve(ctx context.Context, requestReader, replyWriter *os.File, handle func(context.Context, Request) (Confirmation, error)) error {
	if s == nil || !s.used.CompareAndSwap(false, true) {
		return ErrDenied
	}
	if requestReader != nil {
		defer requestReader.Close()
	}
	if replyWriter != nil {
		defer replyWriter.Close()
	}
	var r Request
	if read(ctx, requestReader, time.Time{}, &r) != nil || !r.Valid() || handle == nil {
		return ErrDenied
	}
	ctx, cancel := context.WithDeadline(ctx, r.Deadline)
	defer cancel()
	a, e := handle(ctx, r)
	if e != nil || ctx.Err() != nil || !a.Matches(r) {
		return ErrDenied
	}
	return write(ctx, replyWriter, r.Deadline, a)
}

// InheritedPipe accepts only the two dedicated anonymous permission descriptors.
func InheritedPipe(fd int) (*os.File, error) {
	if fd < 5 || fd > 6 || unix.SetNonblock(fd, true) != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "private-permit")
	if !anonymous(f) {
		if f != nil {
			_ = f.Close()
		}
		return nil, ErrDenied
	}
	return f, nil
}
