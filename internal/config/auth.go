package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Origin struct {
	Scheme, Host string
	Port         int
}
type AuthConfig struct {
	Mode       string
	Origin     Origin
	SessionTTL time.Duration
}

// ParseOrigin accepts only an unambiguous serialized HTTP origin. Neither
// request Host nor any forwarded header supplies trusted configuration.
func ParseOrigin(value string) (Origin, error) {
	invalid := errors.New("must be a single HTTP(S) origin without credentials or path")
	u, err := url.Parse(value)
	if err != nil || value == "" || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(value, " \t\r\n,#%\\") || (u.Scheme != "http" && u.Scheme != "https") {
		return Origin{}, invalid
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(u.Host, ":") {
		return Origin{}, invalid
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		if len(host) > 253 || strings.Contains(host, ":") {
			return Origin{}, invalid
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return Origin{}, invalid
			}
			for _, c := range []byte(label) {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return Origin{}, invalid
				}
			}
		}
		// Numeric dotted hosts which are not canonical IPs are ambiguous.
		if strings.Contains(host, ".") && strings.Trim(host, "0123456789.") == "" {
			return Origin{}, invalid
		}
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if text := u.Port(); text != "" {
		port, err = strconv.Atoi(text)
		if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != text {
			return Origin{}, invalid
		}
	}
	// Reject brackets unless they contain an IPv6 literal.
	if strings.ContainsAny(u.Host, "[]") && (net.ParseIP(host) == nil || !strings.Contains(host, ":")) {
		return Origin{}, invalid
	}
	return Origin{Scheme: u.Scheme, Host: host, Port: port}, nil
}

func loadAuth(getenv func(string) string, listen string) (AuthConfig, error) {
	cfg := AuthConfig{Mode: valueOrDefault(getenv("AUTH_MODE"), "production")}
	if cfg.Mode != "production" && cfg.Mode != "development" {
		return AuthConfig{}, fmt.Errorf("AUTH_MODE: must be production or development")
	}
	if getenv("AUTH_ORIGIN") == "" {
		return AuthConfig{}, fmt.Errorf("AUTH_ORIGIN: required")
	}
	var err error
	if cfg.Origin, err = ParseOrigin(getenv("AUTH_ORIGIN")); err != nil {
		return AuthConfig{}, fmt.Errorf("AUTH_ORIGIN: invalid origin")
	}
	cfg.SessionTTL, err = parsePositiveDuration(getenv("SESSION_TTL"), 24*time.Hour)
	if err != nil || cfg.SessionTTL < 5*time.Minute || cfg.SessionTTL > 24*time.Hour {
		return AuthConfig{}, fmt.Errorf("SESSION_TTL: must be between 5m and 24h")
	}
	if cfg.Mode == "production" {
		if cfg.Origin.Scheme != "https" {
			return AuthConfig{}, fmt.Errorf("AUTH_ORIGIN: production requires HTTPS")
		}
	} else {
		ip := net.ParseIP(cfg.Origin.Host)
		if cfg.Origin.Scheme != "http" || !(cfg.Origin.Host == "localhost" || ip != nil && ip.IsLoopback()) {
			return AuthConfig{}, fmt.Errorf("AUTH_ORIGIN: development requires loopback HTTP")
		}
		host, port, _ := net.SplitHostPort(listen)
		listenIP := net.ParseIP(host)
		n, _ := strconv.Atoi(port)
		if listenIP == nil || !listenIP.IsLoopback() || n != cfg.Origin.Port {
			return AuthConfig{}, fmt.Errorf("HTTP_ADDR: development requires explicit IP loopback and matching origin port")
		}
	}
	return cfg, nil
}
