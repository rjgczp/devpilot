//go:build privateworker && linux

package workerruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClosedControlBoundaryRoutesAndEnvironment(t *testing.T) {
	v4 := "Iface Destination Gateway Flags RefCnt Use Metric Mask\neth0 000012AC 00000000 0001 0 0 0 0000FFFF\n"
	if defaultRoutes(v4, "") {
		t.Fatal("specific PG route rejected")
	}
	if !defaultRoutes(v4+"eth0 00000000 010012AC 0003 0 0 0 00000000\n", "") {
		t.Fatal("v4 default accepted")
	}
	if !defaultRoutes(v4, strings.Repeat("0", 32)+" 00 "+strings.Repeat("0", 32)+" 00 "+strings.Repeat("0", 32)+" 00000001 00000000 00000000 00000001 eth0") {
		t.Fatal("v6 default accepted")
	}
	if !defaultRoutes("broken", "") {
		t.Fatal("unknown routes accepted")
	}
	for _, key := range []string{"DATABASE_URL", "PGPASSWORD", "AWS_ACCESS_KEY_ID", "GOOGLE_APPLICATION_CREDENTIALS", "GITHUB_TOKEN"} {
		if CleanEnvironment([]string{key + "=secret"}) {
			t.Fatal("credential environment accepted")
		}
	}
	if !CleanEnvironment([]string{"PATH=/bin", "LANG=C"}) {
		t.Fatal("normal environment rejected")
	}
}
func TestWorkerCredentialFileAndBudgets(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dsn")
	write := func(s string) {
		t.Helper()
		if e := os.WriteFile(file, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	inc := "00000000-0000-4000-8000-000000000001"
	write("postgres://devpilot_private_worker:sentinel@pg/db?sslmode=disable&statement_timeout=9s&lock_timeout=9s")
	c, e := DatabaseConfig(file, inc)
	if e != nil {
		t.Fatal(e)
	}
	if c.MaxConns != 1 || c.ConnConfig.RuntimeParams["statement_timeout"] != "2s" || c.ConnConfig.RuntimeParams["transaction_timeout"] != "2s" || c.ConnConfig.RuntimeParams["lock_timeout"] != "250ms" {
		t.Fatal("SQL budget changed")
	}
	for _, value := range []string{"postgres://postgres:sentinel@pg/db", "postgres://devpilot_private_app:sentinel@pg/db", "postgres://worker:sentinel@pg/db?options=-cstatement_timeout%3D99s", "postgres://worker@pg/db", strings.Repeat("s", 8193), "secret-sentinel"} {
		write(value)
		if _, e = DatabaseConfig(file, inc); e == nil || strings.Contains(e.Error(), "sentinel") || strings.Contains(e.Error(), file) {
			t.Fatal("unsafe credential configuration accepted or disclosed")
		}
	}
}
