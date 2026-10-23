//go:build integration && networkaudit

package integration

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestMonitorNetworkProbePositiveControl(t *testing.T) {
	// This test exists only to calibrate the syscall classifier in a disposable
	// Linux namespace whose firewall rejects all destinations except DB/local HTTP.
	if os.Getenv("MONITOR_PROBE_ISOLATED") != "1" {
		t.Fatal("isolated Linux audit runner required")
	}
	conn, _ := net.DialTimeout("tcp4", "192.0.2.1:80", 50*time.Millisecond)
	if conn != nil {
		conn.Close()
		t.Fatal("isolation firewall allowed the control")
	}
	// Returning success is intentional: the external probe MUST fail this run
	// based on the recorded attempt, regardless of connect's return value.
	t.Log("intentional disallowed connect attempted; external audit must reject")
}
func TestMonitorNoTargetNetwork(t *testing.T) {
	// Full HTTP journey uses production router/Handler/Store, real PG and real
	// Cookie login plus invitation-backed Member. Audit executes this test alone.
	if os.Getenv("MONITOR_PROBE_ISOLATED") != "1" {
		t.Fatal("isolated Linux audit runner required")
	}
	if os.Getenv("MONITOR_PROBE_HTTP_ADDR") != "127.0.0.1:18081" {
		t.Fatal("exact approved HTTP address required")
	}
	t.Run("http-restart", TestMonitorHTTPJourneyAndIsolation)
	t.Run("unknown-commit-reconciliation", TestActivationCommitFaults)
	t.Run("http-unknown-commit", TestActivationHTTPUnknownCommitAndFaults)
	t.Run("full-workspace-intent-cap", TestActivationQuotaAndCAS)
	t.Run("finite-execution-authorization", TestCheckpointOneAuthorizationRealHTTPStrictContract)
	t.Run("intent-and-authoritative-status", TestCheckpointOneHTTPIntentStatusContract)
}
