//go:build schedulerfixture && linux

package scheduler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

func resultExample() (handoff.Lineage, *monitoring.Result) {
	u := "11111111-1111-4111-8111-111111111111"
	l := handoff.Lineage{WorkspaceID: u, ProjectID: u, MonitorID: u, RunID: u, ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "3", FencingToken: "4", NamespaceIncarnation: u, SupervisorIncarnation: u}
	n := 200
	now := time.Now()
	return l, &monitoring.Result{WorkspaceID: u, ProjectID: u, MonitorID: u, ConfigurationVersion: "1", StartedAt: now, FinishedAt: now.Add(time.Millisecond), Duration: time.Millisecond, HTTPStatus: &n, Success: true}
}
func TestMinimalResultCopiesAndRedacts(t *testing.T) {
	l, r := resultExample()
	v, e := NetworkResult(l, r)
	if e != nil {
		t.Fatal(e)
	}
	*r.HTTPStatus = 599
	r.ConfigurationVersion = "SECRET_RESULT_SENTINEL"
	r.Success = false
	if *v.value.status != 200 || !*v.value.success {
		t.Fatal("mutable result retained")
	}
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		s := fmt.Sprintf(f, v)
		if strings.Contains(s, "11111111") || strings.Contains(s, "SECRET") {
			t.Fatal("diagnostic leak")
		}
	}
	copy := v
	if !v.value.used.CompareAndSwap(false, true) || copy.value.used.CompareAndSwap(false, true) {
		t.Fatal("copied capability reusable")
	}
}
func TestMinimalResultRejectsInvalidFields(t *testing.T) {
	for _, bad := range []string{"nil", "scope", "version", "duration", "status", "failure", "success", "times"} {
		t.Run(bad, func(t *testing.T) {
			l, r := resultExample()
			switch bad {
			case "nil":
				r = nil
			case "scope":
				l.MonitorID = "SECRET_RESULT_SENTINEL"
			case "version":
				r.ConfigurationVersion = "01"
			case "duration":
				r.Duration = 30*time.Second + 1
			case "status":
				*r.HTTPStatus = 0
			case "failure":
				c := monitoring.Failure("raw_error_SECRET_RESULT_SENTINEL")
				r.FailureCode = &c
			case "success":
				r.Success = false
			case "times":
				r.FinishedAt = r.StartedAt.Add(-1)
			}
			if _, e := NetworkResult(l, r); e != ErrResult {
				t.Fatal("invalid result accepted")
			}
		})
	}
}
func TestMinimalControlCannotBecomeHTTP(t *testing.T) {
	l, _ := resultExample()
	for _, v := range [][2]string{{"preflight", "invalid_configuration"}, {"permit_denied", "egress_unavailable"}, {"physical_stop_unconfirmed", "worker_lost"}} {
		r, e := ControlResult(l, v[0], v[1])
		if e != nil || r.value.success != nil || r.value.failure != nil || r.value.duration != nil || r.value.status != nil {
			t.Fatal("control became HTTP", e)
		}
	}
	for _, v := range [][2]string{{"network", "timeout"}, {"preflight", "timeout"}, {"permit_denied", "connect_failed"}, {"physical_stop_unconfirmed", "success"}, {"SECRET_RESULT_SENTINEL", "Cookie=secret"}} {
		if _, e := ControlResult(l, v[0], v[1]); e != ErrResult {
			t.Fatal("invalid control accepted")
		}
	}
	l.NamespaceIncarnation = ""
	l.SupervisorIncarnation = ""
	if _, e := ControlResult(l, "preflight", "invalid_configuration"); e != nil {
		t.Fatal("unprepared control", e)
	}
}
