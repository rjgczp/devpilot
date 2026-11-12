package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitDatabaseSecretFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "database")
	if err := os.WriteFile(file, []byte(testDSN+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(env(map[string]string{"DATABASE_URL_FILE": file}))
	if err != nil || cfg.Database.ConnConfig.User != "devpilot" || cfg.Database.ConnConfig.Password != "top-secret" {
		t.Fatal("explicit database secret not loaded")
	}
	for _, tc := range []struct{ name, content, direct string }{
		{"conflict", testDSN, testDSN},
		{"empty", "", ""},
		{"oversize", strings.Repeat("x", 8193), ""},
		{"multiline", testDSN + "\n" + testDSN, ""},
		{"invalid", "secret-sentinel", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := os.WriteFile(file, []byte(tc.content), 0o600); e != nil {
				t.Fatal(e)
			}
			_, e := Load(env(map[string]string{"DATABASE_URL_FILE": file, "DATABASE_URL": tc.direct}))
			if e == nil || strings.Contains(e.Error(), file) || strings.Contains(e.Error(), "top-secret") || strings.Contains(e.Error(), "sentinel") {
				t.Fatal("secret configuration must fail without exposing path or content")
			}
		})
	}
	_, err = Load(env(map[string]string{"DATABASE_URL_FILE": filepath.Join(t.TempDir(), "missing-secret-sentinel")}))
	if err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("missing secret must fail without exposing path")
	}
}
