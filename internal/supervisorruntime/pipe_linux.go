//go:build privateworker && privateprobe && linux

// Package supervisorruntime wires existing roles and the accepted physical
// verifier. There is no listener, role provisioning, SQL transport or GET retry.
package supervisorruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/probenetwork"
	"github.com/rjgczp/devpilot/internal/storagecontrol"
	"github.com/rjgczp/devpilot/internal/workerruntime"
	"io"
	"os"
	"sync"
	"time"
)

var ErrClosed = errors.New("private control service closed")

const frameLimit = 64 << 10

type BackendReply struct {
	Event        string                     `json:"event"`
	Line         handoff.Lineage            `json:"line"`
	Ready        []json.RawMessage          `json:"ready,omitempty"`
	Frame        []byte                     `json:"frame,omitempty"`
	Confirmation *targetpermit.Confirmation `json:"confirmation,omitempty"`
	Observation  *monitoring.Result         `json:"observation,omitempty"`
	Receipt      []byte                     `json:"receipt,omitempty"`
	Bundle       []byte                     `json:"bundle,omitempty"`
}

func decode(raw []byte, value any) error {
	if len(raw) > frameLimit || !credentials.ValidJSON(raw) || probenetwork.UniqueJSON(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return ErrClosed
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return ErrClosed
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrClosed
	}
	return nil
}

type Peer struct {
	in, out *os.File
	reader  *bufio.Reader
	mu      sync.Mutex
}

func InheritedPeer(readFD, writeFD int) (*Peer, error) {
	if readFD == writeFD {
		return nil, ErrClosed
	}
	in, e := storagecontrol.InheritedPipe(readFD, false)
	if e != nil {
		return nil, ErrClosed
	}
	out, e := storagecontrol.InheritedPipe(writeFD, true)
	if e != nil {
		in.Close()
		return nil, ErrClosed
	}
	return &Peer{in: in, out: out, reader: bufio.NewReaderSize(in, frameLimit+1)}, nil
}
func (p *Peer) Close() { p.in.Close(); p.out.Close() }
func (p *Peer) Call(ctx context.Context, c workerruntime.Call) (BackendReply, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var reply BackendReply
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if ctx.Err() != nil || p.in.SetReadDeadline(deadline) != nil || p.out.SetWriteDeadline(deadline) != nil {
		return reply, ErrClosed
	}
	stop := context.AfterFunc(ctx, p.Close)
	defer stop()
	raw, e := json.Marshal(c)
	if e != nil || len(raw) >= frameLimit {
		return reply, ErrClosed
	}
	if _, e = p.out.Write(append(raw, '\n')); e != nil {
		return reply, ErrClosed
	}
	raw, e = p.reader.ReadSlice('\n')
	if e != nil || decode(raw, &reply) != nil || ctx.Err() != nil {
		return BackendReply{}, ErrClosed
	}
	if c.Line != nil && reply.Line != *c.Line {
		return BackendReply{}, ErrClosed
	}
	return reply, nil
}

// Serve performs only fixed typed calls. Idle IPC holds no database connection.
func Serve(ctx context.Context, p *Peer, handle func(context.Context, workerruntime.Call) (workerruntime.Reply, error)) error {
	defer p.Close()
	stop := context.AfterFunc(ctx, p.Close)
	defer stop()
	for {
		raw, e := p.reader.ReadSlice('\n')
		if e != nil {
			return ErrClosed
		}
		var call workerruntime.Call
		if decode(raw, &call) != nil {
			return ErrClosed
		}
		reply, e := handle(ctx, call)
		if e != nil {
			return e
		}
		raw, e = json.Marshal(reply)
		if e != nil || len(raw) >= frameLimit {
			return ErrClosed
		}
		if p.out.SetWriteDeadline(time.Now().Add(2*time.Second)) != nil {
			return ErrClosed
		}
		if _, e = p.out.Write(append(raw, '\n')); e != nil {
			return ErrClosed
		}
	}
}

// ControlClient exposes only the existing typed control reply to Supervisor.
type ControlClient struct{ Peer *Peer }

func (c ControlClient) Call(ctx context.Context, call workerruntime.Call) (workerruntime.Reply, error) {
	r, e := c.Peer.Call(ctx, call)
	return workerruntime.Reply{Event: r.Event, Line: r.Line}, e
}
