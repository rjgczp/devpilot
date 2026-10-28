//go:build schedulerfixture && linux

// This input-only fixture never calls a resolver, dialer, HTTP client or engine.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/probesandbox"
)

func run() error {
	if len(os.Args) != 1 {
		return handoff.ErrInput
	}
	input, err := handoff.InheritedPipe(3)
	if err != nil {
		return err
	}
	receipt, err := handoff.InheritedPipe(4)
	if err != nil {
		input.Close()
		return err
	}
	proof, err := probesandbox.Seal()
	if err != nil {
		return err
	}
	expected, err := handoff.ReadReceipt(receipt)
	if err != nil {
		return err
	}
	receiver := handoff.NewReceiver(expected)
	sealed, err := monitoring.ReceiveSchedulerInput(receiver, input)
	if err != nil || !sealed.Valid() {
		return handoff.ErrInput
	}
	if _, err = receiver.Receive(input); err == nil {
		return handoff.ErrInput
	}
	// Output contains only boolean/numeric boundary evidence, never input/config.
	if err = json.NewEncoder(os.Stdout).Encode(struct {
		Proof               probesandbox.Proof `json:"proof"`
		ConsumedOnce        bool               `json:"consumed_once"`
		KernelSnapshotValid bool               `json:"kernel_snapshot_valid"`
	}{proof, true, sealed.Valid()}); err != nil {
		return handoff.ErrInput
	}
	var release [1]byte
	_, err = io.ReadFull(os.Stdin, release[:])
	return err
}
func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "probe input rejected")
		os.Exit(2)
	}
}
