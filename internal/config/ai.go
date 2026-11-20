package config

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// AIConfig is server-only. An entirely empty configuration disables diagnosis.
type AIConfig struct {
	BaseURL, Model, APIKey string
	Timeout                time.Duration
}

func loadAI(getenv func(string) string) (AIConfig, error) {
	c := AIConfig{BaseURL: getenv("AI_BASE_URL"), Model: getenv("AI_MODEL"), APIKey: getenv("AI_API_KEY"), Timeout: 20 * time.Second}
	file := getenv("AI_API_KEY_FILE")
	if c.BaseURL == "" && c.Model == "" && c.APIKey == "" && file == "" && getenv("AI_TIMEOUT") == "" {
		return c, nil
	}
	invalid := func() (AIConfig, error) {
		return AIConfig{}, fmt.Errorf("AI configuration invalid: require fixed HTTPS base URL, model, one server key source and timeout <=30s")
	}
	if file != "" {
		if c.APIKey != "" {
			return invalid()
		}
		f, e := os.Open(file)
		if e != nil {
			return invalid()
		}
		b, e := io.ReadAll(io.LimitReader(f, 8193))
		_ = f.Close()
		if e != nil || len(b) > 8192 {
			return invalid()
		}
		c.APIKey = strings.TrimSpace(string(b))
	}
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return invalid()
	}
	// Loopback HTTP supports local compatible providers and isolated model mocks.
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return invalid()
	}
	if len(c.BaseURL) > 2048 || len(c.Model) == 0 || len(c.Model) > 128 || strings.ContainsAny(c.Model, "\r\n") || len(c.APIKey) == 0 || len(c.APIKey) > 8192 || strings.ContainsAny(c.APIKey, "\r\n") {
		return invalid()
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if raw := getenv("AI_TIMEOUT"); raw != "" {
		c.Timeout, e = time.ParseDuration(raw)
		if e != nil || c.Timeout <= 0 || c.Timeout > 30*time.Second {
			return invalid()
		}
	}
	return c, nil
}
