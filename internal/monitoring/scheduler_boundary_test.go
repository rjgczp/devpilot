package monitoring

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureSchedulerBuildBoundary(t *testing.T) {
	// Inspect the Linux release graph too: a Darwin-only exclusion would not
	// protect deployed binaries. Environment values cannot change Go build tags.
	cmd := exec.Command("go", "list", "-deps", "./cmd/api")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0", "DEVPILOT_SCHEDULER_ENABLED=true", "DEVPILOT_PROBE_ENABLED=true")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("release dependency inventory failed")
	}
	for _, name := range []string{"/internal/scheduler", "/internal/monitoring"} {
		if strings.Contains(string(b), name) {
			t.Fatal("release execution dependency", name)
		}
	}
	shared := map[string]bool{
		"internal/scheduler/doc.go":                      true,
		"internal/scheduler/input_linux.go":              true,
		"internal/scheduler/recover_started_linux.go":    true,
		"internal/scheduler/recovery_linux.go":           true,
		"internal/scheduler/result_commit_linux.go":      true,
		"internal/scheduler/result_linux.go":             true,
		"internal/scheduler/retention_linux.go":          true,
		"internal/scheduler/start_linux.go":              true,
		"internal/scheduler/store_linux.go":              true,
		"internal/scheduler/target_linux.go":             true,
		"internal/scheduler/target_permit_linux.go":      true,
		"internal/scheduler/worker_linux.go":             true,
		"internal/scheduler/worker_closed_linux_test.go": true,
		"internal/monitoring/handoff/wire_linux.go":      true,
		"internal/monitoring/targetpermit/pipe_linux.go": true,
	}
	for _, dir := range []string{"cmd/privateworker", "internal/workerruntime", "internal/scheduler", "cmd/schedulerfixture", "cmd/probeinputfixture", "cmd/schedulerprobefixture", "internal/monitoring/handoff", "internal/monitoring/probesandbox", "internal/monitoring/targetpermit"} {
		files, err := filepath.Glob(filepath.Join("../..", dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatal("fixture boundary missing")
		}
		for _, file := range files {
			b, err := os.ReadFile(file)
			tag := "schedulerfixture && linux"
			if shared[filepath.ToSlash(strings.TrimPrefix(file, "../../"))] {
				tag = "(schedulerfixture || privateworker) && linux"
			} else if dir == "cmd/privateworker" || dir == "internal/workerruntime" {
				tag = "privateworker && linux"
			}
			if err != nil || !strings.HasPrefix(string(b), "//go:build "+tag+"\n") {
				t.Fatal("execution build boundary changed", file)
			}
		}
	}
	for _, entry := range []struct{ path, tag string }{{"./cmd/schedulerfixture", "schedulerfixture"}, {"./cmd/privateworker", "privateworker"}} {
		for _, tags := range []string{"", "schedulerfixture", "privateworker"} {
			args := []string{"list", "-e", "-json"}
			if tags != "" {
				args = append(args, "-tags", tags)
			}
			args = append(args, entry.path)
			cmd = exec.Command("go", args...)
			cmd.Dir = "../.."
			cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
			b, err = cmd.Output()
			if err != nil {
				t.Fatal("fixture inventory failed")
			}
			var pkg struct {
				GoFiles []string
				Error   *struct{ Err string }
			}
			if json.Unmarshal(b, &pkg) != nil {
				t.Fatal("invalid inventory")
			}
			if tags != entry.tag && (len(pkg.GoFiles) != 0 || pkg.Error == nil) {
				t.Fatal("command exists without its explicit Linux build tag", entry.path, tags)
			}
			if tags == entry.tag && (len(pkg.GoFiles) == 0 || pkg.Error != nil) {
				t.Fatal("explicit execution build unavailable", entry.path, tags)
			}
		}
	}
}

func TestReleaseArtifactAndProbeTestBoundary(t *testing.T) {
	probe, err := os.ReadFile("execution_network_test.go")
	if err != nil || !bytes.HasPrefix(probe, []byte("//go:build executionnet && linux\n")) {
		t.Fatal("Probe test entry must be Linux-only")
	}
	// Inspect a real ordinary Linux API artifact, not only the host's build graph.
	bin := filepath.Join(t.TempDir(), "api")
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0", "EGRESS_APPROVED=1", "DEVPILOT_SCHEDULER_ENABLED=true", "DEVPILOT_PROBE_ENABLED=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build boundary %v failed: %s", args, out)
		}
		return out
	}
	run("build", "-o", bin, "./cmd/api")
	symbols := run("tool", "nm", bin)
	for _, forbidden := range [][]byte{[]byte("devpilot/internal/monitoring."), []byte("devpilot/internal/scheduler.")} {
		if bytes.Contains(symbols, forbidden) {
			t.Fatal("release binary contains execution implementation")
		}
	}
	for _, tags := range []string{"", "executionnet"} {
		args := []string{"list", "-json"}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, "./internal/monitoring")
		var p struct{ GoFiles, TestGoFiles []string }
		if json.Unmarshal(run(args...), &p) != nil {
			t.Fatal("invalid package inventory")
		}
		for _, f := range p.GoFiles {
			if f == "execution_network_test.go" || f == "bridge_test.go" {
				t.Fatal("Probe or snapshot bridge in ordinary library")
			}
		}
		found := false
		for _, f := range p.TestGoFiles {
			if f == "execution_network_test.go" {
				found = true
			}
		}
		if found != (tags == "executionnet") {
			t.Fatal("Probe requires explicit test build")
		}
	}
}
