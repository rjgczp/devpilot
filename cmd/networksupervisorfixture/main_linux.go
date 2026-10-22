//go:build schedulerfixture && linux

// This command is only an isolated fixture management process. Probe receives
// neither its stdio channel nor its NET_ADMIN capability. There is no listener.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rjgczp/devpilot/internal/supervisorfixture"
)

type message struct {
	Op      string                     `json:"op"`
	Config  *supervisorfixture.Config  `json:"config,omitempty"`
	Install *supervisorfixture.Install `json:"install,omitempty"`
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "fingerprint" {
		r, a, e := supervisorfixture.Fingerprints(context.Background())
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
		return supervisorfixture.ErrDenied
	}
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), 4097)
	var p *supervisorfixture.Policy
	output := json.NewEncoder(os.Stdout)
	for s.Scan() {
		var m message
		if supervisorfixture.Decode(s.Bytes(), &m) != nil {
			return supervisorfixture.ErrDenied
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var answer any
		var e error
		switch {
		case m.Op == "boot" && p == nil && m.Config != nil && m.Install == nil:
			p, e = supervisorfixture.Boot(ctx, *m.Config)
			if e == nil {
				answer = p.Ready()
			}
		case m.Op == "revoke" && m.Config != nil && m.Install == nil:
			answer, e = supervisorfixture.Revoke(ctx, *m.Config)
		case m.Op == "install" && p != nil && m.Config == nil && m.Install != nil:
			answer, e = p.Install(ctx, *m.Install)
		default:
			e = supervisorfixture.ErrDenied
		}
		cancel()
		if e != nil {
			return e
		}
		if output.Encode(answer) != nil {
			return supervisorfixture.ErrDenied
		}
	}
	// EOF never installs/renews a permit. Kernel deadlines remain independent
	// of this process, and this command never signs a physical-release receipt.
	return s.Err()
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, supervisorfixture.FailureCode(e))
		os.Exit(2)
	}
}
