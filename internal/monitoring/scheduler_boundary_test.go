package monitoring

import (
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
	for _, dir := range []string{"internal/scheduler", "cmd/schedulerfixture"} {
		files, err := filepath.Glob(filepath.Join("../..", dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatal("fixture boundary missing")
		}
		for _, file := range files {
			b, err := os.ReadFile(file)
			if err != nil || !strings.HasPrefix(string(b), "//go:build schedulerfixture && linux\n") {
				t.Fatal("ordinary build can compile fixture entry", file)
			}
		}
	}
	for _, tags := range []string{"", "schedulerfixture"} {
		args := []string{"list", "-e", "-json"}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, "./cmd/schedulerfixture")
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
		if tags == "" && (len(pkg.GoFiles) != 0 || pkg.Error == nil) {
			t.Fatal("fixture command exists in ordinary Linux build")
		}
		if tags != "" && (len(pkg.GoFiles) == 0 || pkg.Error != nil) {
			t.Fatal("explicit fixture build unavailable")
		}
	}
}
