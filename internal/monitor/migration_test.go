package monitor

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

func TestHistoricalMigrationsAreImmutable(t *testing.T) {
	for name, want := range map[string]string{
		"00007_create_monitor_activation_states.sql": "713c39b60a3ede8156c1163404a6883cea00b6c8979b94b36a96047a5532cfaa",
		"00001_create_users.sql":                     "e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490",
		"00002_create_sessions.sql":                  "27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a",
		"00003_create_workspaces.sql":                "327651e7727c512d80062792bab0ea874f1f63320b98ca6b0491dd8e6f94223d",
		"00004_create_workspace_invitations.sql":     "0374475bb78a985a9baf3de72055032bcc08e6f18512b850442a59940f20fdfb",
		"00005_create_projects.sql":                  "4715106905a92991c7f1dcb2da08afb7fd93217716bfa83eb4a33bb8145ac519",
		"00006_create_http_monitors.sql":             "26ac26c66da96b0f16076342f0f11258bb6ea92be5f71a6b015c97a3075ee4e2",
	} {
		b, e := os.ReadFile("../../migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != want {
			t.Error("historical migration changed", name)
		}
	}
}
