package config

import (
	"strings"
	"testing"
	"time"
)

func TestAuthDefaultsLocalAndInvalidPolicies(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDSN}))
	if err != nil || cfg.Auth.Mode != "production" || cfg.Auth.SessionTTL != 24*time.Hour || cfg.Auth.Origin.Scheme != "https" {
		t.Fatal("secure defaults")
	}
	for _, origin := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080"} {
		cfg, err = Load(env(map[string]string{"DATABASE_URL": testDSN, "AUTH_MODE": "development", "AUTH_ORIGIN": origin, "HTTP_ADDR": "127.0.0.1:8080", "SESSION_TTL": "5m"}))
		if err != nil || cfg.Auth.SessionTTL != 5*time.Minute {
			t.Fatal("local HTTP policy")
		}
	}
	for _, tc := range []struct{ key, value string }{
		{"AUTH_ORIGIN", ""}, {"AUTH_MODE", "test"}, {"SESSION_TTL", "4m59s"}, {"SESSION_TTL", "24h1s"}, {"SESSION_TTL", "bad"}, {"SESSION_TTL", "0s"},
		{"AUTH_ORIGIN", "http://example.com"}, {"AUTH_ORIGIN", "https://user:sentinel@example.com"}, {"AUTH_ORIGIN", "https://example.com/"}, {"AUTH_ORIGIN", "https://example.com?"}, {"AUTH_ORIGIN", "https://example.com#x"}, {"AUTH_ORIGIN", "https://*.example.com"}, {"AUTH_ORIGIN", "https://example.com:65536"}, {"AUTH_ORIGIN", "https://example.com:01"}, {"AUTH_ORIGIN", "https://example.com:"}, {"AUTH_ORIGIN", "https://127.000.0.1"}, {"AUTH_ORIGIN", "https://[example.com]"}, {"AUTH_ORIGIN", "https://[::1%25en0]"}, {"AUTH_ORIGIN", "https://example.com,https://attacker.com"},
	} {
		values := map[string]string{"DATABASE_URL": testDSN, tc.key: tc.value}
		if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), "sentinel") || strings.Contains(err.Error(), "top-secret") {
			t.Fatalf("unsafe or accepted %s policy", tc.key)
		}
	}
	for _, tc := range []struct{ origin, addr string }{
		{"http://127.0.0.1:8080", ":8080"}, {"http://127.0.0.1:8080", "0.0.0.0:8080"}, {"http://127.0.0.1:8080", "localhost:8080"}, {"http://127.0.0.1:8080", "127.0.0.1:8081"}, {"http://192.168.0.1:8080", "127.0.0.1:8080"}, {"https://localhost:8080", "127.0.0.1:8080"},
	} {
		if _, err := Load(env(map[string]string{"DATABASE_URL": testDSN, "AUTH_MODE": "development", "AUTH_ORIGIN": tc.origin, "HTTP_ADDR": tc.addr})); err == nil {
			t.Fatal("unsafe development policy accepted")
		}
	}
}

func TestOriginCompleteTuple(t *testing.T) {
	a, err := ParseOrigin("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseOrigin("https://EXAMPLE.com:443")
	if err != nil || a != b {
		t.Fatal("effective port or hostname case")
	}
	for _, other := range []string{"http://example.com", "https://example.com:444", "https://example.com.attacker.com", "https://sub.example.com"} {
		c, err := ParseOrigin(other)
		if err != nil || c == a {
			t.Fatal("origin suffix or tuple match")
		}
	}
}
