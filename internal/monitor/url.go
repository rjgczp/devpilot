package monitor

import (
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// This profile is pure computation. Configuration admission never resolves DNS
// or proves that a host is safe to connect to. A future prober must do both.
var dnsProfile = idna.New(idna.ValidateForRegistration(), idna.Transitional(false),
	idna.ValidateLabels(true), idna.StrictDomainName(true), idna.CheckHyphens(true),
	idna.CheckJoiners(true), idna.BidiRule(), idna.VerifyDNSLength(true))

// canonicalURL preserves the escaped path, including encoded delimiters and dot
// segments. Do not serialize u.Path (decoded), clean it, or resolve a reference.
func canonicalURL(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 2048 {
		return "", false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] <= 32 || raw[i] >= 127 || raw[i] == '\\' {
			return "", false
		}
	}
	if strings.ContainsAny(raw, "?#") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || !strings.HasPrefix(strings.ToLower(raw), scheme+"://") {
		return "", false
	}
	authority := u.Host
	if strings.ContainsAny(authority, "@%[]") {
		return "", false
	}
	host := authority
	if i := strings.IndexByte(authority, ':'); i >= 0 {
		host = authority[:i]
		port := authority[i+1:]
		if !(scheme == "http" && port == "80" || scheme == "https" && port == "443") {
			return "", false
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if _, err := netip.ParseAddr(host); err == nil {
		return "", false
	}
	if !validHost(host) {
		return "", false
	}
	path := raw[len(u.Scheme)+3+len(u.Host):]
	if path == "" {
		path = "/"
	}
	if path[0] != '/' {
		return "", false
	}
	b := []byte(path)
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == '%' {
			if i+2 >= len(b) {
				return "", false
			}
			a, ok := unhex(b[i+1])
			if !ok {
				return "", false
			}
			z, ok := unhex(b[i+2])
			if !ok {
				return "", false
			}
			v := a*16 + z
			if v < 32 || v == 127 || v == '\\' {
				return "", false
			}
			b[i+1], b[i+2] = hex[a], hex[z]
			i += 2
		} else if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~!$&'()*+,;=:@/", rune(c))) {
			return "", false
		}
	}
	out := scheme + "://" + host + string(b)
	// Check the exact serialization without allowing net/url to decode and then
	// re-escape a different path (e.g. %2F versus /, %41 versus A).
	check, err := url.Parse(out)
	if err != nil || len(out) > 2048 || check.Scheme != scheme || check.Host != host || check.EscapedPath() != string(b) {
		return "", false
	}
	return out, true
}
func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
func validHost(host string) bool {
	if len(host) > 253 {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, s := range labels {
		if len(s) == 0 || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
			return false
		}
		for _, c := range s {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
		if len(s) >= 4 && s[2:4] == "--" && !strings.HasPrefix(s, "xn--") {
			return false
		}
	}
	last := labels[len(labels)-1]
	if !strings.HasPrefix(last, "xn--") {
		if len(last) < 2 {
			return false
		}
		for _, c := range last {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	for _, suffix := range []string{"localhost", "local", "internal", "home.arpa", "invalid", "test", "example", "onion"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return false
		}
	}
	unicode, err := dnsProfile.ToUnicode(host)
	if err != nil {
		return false
	}
	ascii, err := dnsProfile.ToASCII(unicode)
	return err == nil && ascii == host
}
