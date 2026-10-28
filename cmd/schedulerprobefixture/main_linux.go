//go:build schedulerfixture && linux

// The only executable bridge is an explicit Linux fixture build. It accepts no
// arguments, configuration environment, DB identity or management connection.
package main

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/probesandbox"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
)

func run() error {
	if len(os.Args) != 1 {
		return handoff.ErrInput
	}
	in, err := handoff.InheritedPipe(3)
	if err != nil {
		return err
	}
	ack, err := handoff.InheritedPipe(4)
	if err != nil {
		return err
	}
	req, err := targetpermit.InheritedPipe(5)
	if err != nil {
		return err
	}
	reply, err := targetpermit.InheritedPipe(6)
	if err != nil {
		return err
	}
	proof, err := probesandbox.SealNetwork()
	if err != nil {
		return err
	}
	expected, err := handoff.ReadReceipt(ack)
	if err != nil {
		return err
	}
	receiver := handoff.NewReceiver(expected)
	input, err := monitoring.ReceiveSchedulerInput(receiver, in)
	if err != nil {
		return err
	}
	if _, err = receiver.Receive(in); err == nil {
		return handoff.ErrInput
	}
	out := json.NewEncoder(os.Stdout)
	if out.Encode(struct {
		Event string             `json:"event"`
		Proof probesandbox.Proof `json:"proof"`
	}{"sealed", proof}) != nil {
		return handoff.ErrInput
	}
	result, err := input.ExecuteFixture(context.Background(), targetpermit.NewClient(req, reply))
	reason := ""
	if err != nil {
		reason = "execution_not_observed"
	}
	if out.Encode(struct {
		Event      string             `json:"event"`
		Result     *monitoring.Result `json:"result"`
		StopReason string             `json:"stop_reason"`
	}{"observation", result, reason}) != nil {
		return handoff.ErrInput
	}
	// Stay in the original process domain until independently killed. Exit/result
	// is never a substitute for network revoke, pidfd/reap and namespace retirement.
	var b [1]byte
	_, err = io.ReadFull(os.Stdin, b[:])
	return err
}

func main() {
	if run() != nil {
		os.Stderr.WriteString("probe rejected\n")
		os.Exit(2)
	}
}
