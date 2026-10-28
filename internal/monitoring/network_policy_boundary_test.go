package monitoring

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNetworkSupervisorFixtureIsAbsentFromRelease(t *testing.T) {
	for _, path := range []string{"./cmd/networksupervisorfixture", "./internal/supervisorfixture"} {
		cmd := exec.Command("go", "list", "-e", "-json", path)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0", "EGRESS_APPROVED=1", "DEVPILOT_SCHEDULER_ENABLED=true")
		out, e := cmd.Output()
		if e != nil {
			t.Fatal(e)
		}
		var p struct {
			GoFiles, IgnoredGoFiles []string
			Error                   *struct{ Err string }
		}
		if json.Unmarshal(out, &p) != nil || len(p.GoFiles) != 0 || len(p.IgnoredGoFiles) == 0 || p.Error == nil || !strings.Contains(p.Error.Err, "build constraints exclude all Go files") {
			t.Fatal("network management entered ordinary Linux release", path)
		}
	}
	cmd := exec.Command("go", "list", "-deps", "./cmd/api")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
	out, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(out), "devpilot/internal/supervisorfixture") {
		t.Fatal("API gained network management dependency")
	}
}
