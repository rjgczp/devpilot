package monitoring

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testScope(n int) Scope {
	return Scope{"01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", fmt.Sprintf("01900000-0000-7000-8000-%012x", n+10)}
}
func testSnapshot(n int) Snapshot {
	return Snapshot{scope: testScope(n), name: "fixture", target: "http://probe-a.example.com/secret-sentinel", method: "GET", interval: 60, timeout: 1, expected: 200, version: 7}
}

func TestImmutableSnapshotResultContract(t *testing.T) {
	s := testSnapshot(1)
	if !s.valid() {
		t.Fatal("valid fixture")
	}
	copy := s
	copy.target = "http://other.example.com/"
	if s.target == copy.target {
		t.Fatal("snapshot mutation")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if strings.Contains(fmt.Sprintf(format, s), "sentinel") {
			t.Fatal("fmt leak")
		}
	}
	b, _ := json.Marshal(s)
	if string(b) != "{}" {
		t.Fatal("snapshot JSON leak")
	}
	for _, mutate := range []func(*Snapshot){func(s *Snapshot) { s.version = 0 }, func(s *Snapshot) { s.target = "http://127.0.0.1/" }, func(s *Snapshot) { s.target = "http://EXAMPLE.COM/" }, func(s *Snapshot) { s.method = "POST" }, func(s *Snapshot) { s.timeout = 31 }, func(s *Snapshot) { s.scope.ProjectID = "" }} {
		bad := s
		mutate(&bad)
		if bad.valid() {
			t.Fatal("invalid snapshot accepted")
		}
	}
	start := time.Now()
	status := 302
	r := result(s, start, start.Add(25*time.Millisecond), &status, UnexpectedStatus)
	if r.Duration != 25*time.Millisecond || r.StartedAt.Location() != time.UTC || r.FinishedAt.Location() != time.UTC || r.ConfigurationVersion != "7" || r.HTTPStatus == nil || *r.HTTPStatus != 302 || r.Success || *r.FailureCode != UnexpectedStatus {
		t.Fatal("result contract")
	}
	want := []string{"workspace_id", "project_id", "monitor_id", "configuration_version", "started_at", "finished_at", "duration", "http_status", "success", "failure_code"}
	b, _ = json.Marshal(r)
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if len(fields) != len(want) {
		t.Fatal("projection")
	}
	for _, k := range want {
		if _, ok := fields[k]; !ok {
			t.Fatal(k)
		}
	}
	if strings.Contains(string(b), "sentinel") {
		t.Fatal("result leak")
	}
	if result(s, start, start, nil, "").FailureCode != nil {
		t.Fatal("success failure")
	}
	if reflect.TypeOf(Snapshot{}).NumField() != 8 {
		t.Fatal("review snapshot fields")
	}
}

func TestDormantProductionDependencyAndSchemaBoundary(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./cmd/api")
	cmd.Dir = "../.."
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("dependency inventory failed")
	}
	if strings.Contains(string(b), "/internal/monitoring") {
		t.Fatal("API imports prober")
	}
	for _, p := range []string{"cmd/api/main.go", "internal/app/routes.go", "internal/app/runner.go"} {
		b, e := os.ReadFile(filepath.Join("../..", p))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "monitoring") {
			t.Fatal("production trigger", p)
		}
	}
	b, _ = os.ReadFile("../../cmd/api/main.go")
	if !strings.Contains(string(b), "Workers:         []func(context.Context){cleanup.Run}") {
		t.Fatal("review worker startup")
	}
	files, _ := filepath.Glob("../../migrations/*.sql")
	// Migration9 is separately approved by Task8.1; no ordinary execution entry.
	if len(files) != 9 || filepath.Base(files[8]) != "00009_enforce_scheduler_capacity.sql" {
		t.Fatal("unreviewed migration")
	}
	// Startup wiring remains byte-identical. The approved schema8 API additions
	// are validated by monitor package OpenAPI response/shape contract tests.
	for p, want := range map[string]string{
		"internal/app/routes.go":                        "96d4192f3ab0e92ed1b49b4b8cee07393d329cf83d8538b6139ee3844a97fc38",
		"migrations/00008_create_monitor_scheduler.sql": "7f56196a3e6b5e2c4bb32dc8e7382c177435d60da833191ad994349b29f417f2",
	} {
		b, err := os.ReadFile(filepath.Join("../..", p))
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != want {
			t.Fatal("API contract changed", p)
		}
	}
}
