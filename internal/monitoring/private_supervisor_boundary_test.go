package monitoring

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestPrivateSupervisorRequiresBothExplicitLinuxTags(t *testing.T) {
	for _, tags := range []string{"", "schedulerfixture", "privateworker", "privateprobe", "privateworker,privateprobe"} {
		args := []string{"list", "-e", "-json"}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, "./cmd/privatesupervisor")
		cmd := exec.Command("go", args...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
		b, e := cmd.Output()
		if e != nil {
			t.Fatal(e)
		}
		var p struct {
			GoFiles []string
			Error   *struct{ Err string }
		}
		if json.Unmarshal(b, &p) != nil {
			t.Fatal("inventory")
		}
		available := len(p.GoFiles) > 0 && p.Error == nil
		if available != (tags == "privateworker,privateprobe") {
			t.Fatal("Supervisor appeared outside explicit private composition", tags)
		}
	}
}
