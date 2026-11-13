//go:build privateprobe && linux

// Private default-deny namespace management. Probe receives neither its
// management channel nor NET_ADMIN capability. No DB identity or listener.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rjgczp/devpilot/internal/probenetwork"
)

type message struct {
	Op      string                `json:"op"`
	Config  *probenetwork.Config  `json:"config,omitempty"`
	Install *probenetwork.Install `json:"install,omitempty"`
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "fingerprint" {
		r, a, e := probenetwork.Fingerprints(context.Background())
		if e != nil {
			return e
		}
		ns, e := os.Readlink("/proc/self/ns/net")
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"namespace": ns, "route_hash": r, "address_hash": a})
	}
	if len(os.Args) != 1 {
		return probenetwork.ErrDenied
	}
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), 4097)
	var p *probenetwork.Policy
	output := json.NewEncoder(os.Stdout)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if p != nil {
			_ = p.Close(ctx)
		}
	}()
	for s.Scan() {
		var m message
		if probenetwork.Decode(s.Bytes(), &m) != nil {
			return probenetwork.ErrDenied
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var answer any
		var e error
		switch {
		case m.Op == "boot" && p == nil && m.Config != nil && m.Install == nil:
			p, e = probenetwork.BootPrivate(ctx, *m.Config)
			if e == nil {
				answer = p.Ready()
			}
		case m.Op == "revoke" && m.Config != nil && m.Install == nil:
			answer, e = probenetwork.Revoke(ctx, *m.Config)
		case m.Op == "install" && p != nil && m.Config == nil && m.Install != nil:
			answer, e = p.Install(ctx, *m.Install)
		default:
			e = probenetwork.ErrDenied
		}
		cancel()
		if e != nil {
			return e
		}
		if output.Encode(answer) != nil {
			return probenetwork.ErrDenied
		}
	}
	// EOF never installs/renews a permit. Kernel deadlines remain independent
	// of this process, and this command never signs a physical-release receipt.
	return s.Err()
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, probenetwork.FailureCode(e))
		os.Exit(2)
	}
}
