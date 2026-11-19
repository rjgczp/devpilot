package config

import (
	"strings"
	"testing"
)

func TestAIConfig(t *testing.T) {
	for _, tc := range []struct {
		name, url, model, key, timeout string
		ok                             bool
	}{
		{name: "disabled", ok: true},
		{name: "https", url: "https://llm.example.com/v1", model: "model", key: "private", ok: true},
		{name: "loopback", url: "http://127.0.0.1:9999/v1", model: "local", key: "private", ok: true},
		{name: "remote_http", url: "http://llm.example.com/v1", model: "model", key: "private"},
		{name: "partial", url: "https://llm.example.com/v1"},
		{name: "userinfo", url: "https://secret@llm.example.com/v1", model: "model", key: "private"},
		{name: "query", url: "https://llm.example.com/v1?key=secret", model: "model", key: "private"},
		{name: "long_timeout", url: "https://llm.example.com/v1", model: "model", key: "private", timeout: "31s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"AI_BASE_URL": tc.url, "AI_MODEL": tc.model, "AI_API_KEY": tc.key, "AI_TIMEOUT": tc.timeout}
			_, e := loadAI(func(k string) string { return env[k] })
			if (e == nil) != tc.ok {
				t.Fatal(e)
			}
			if e != nil && strings.Contains(e.Error(), "private") {
				t.Fatal("secret error")
			}
		})
	}
}
