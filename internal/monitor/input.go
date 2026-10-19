// Package monitor is the sole supported writer of HTTP Monitor configuration.
// It never resolves, connects to, or probes a configured target.
package monitor

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rjgczp/devpilot/internal/credentials"
)

const MaxBodyBytes = 16384

type Input struct {
	Name, URL, Method                               *string
	IntervalSeconds, TimeoutSeconds, ExpectedStatus *int64
}

func parseInput(b []byte, create bool) (Input, bool) {
	var v Input
	if !credentials.ValidJSON(b) {
		return v, false
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return v, false
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return v, false
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return v, false
		}
		seen[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || len(raw) == 0 {
			return v, false
		}
		var text **string
		var number **int64
		switch key {
		case "name":
			text = &v.Name
		case "url":
			text = &v.URL
		case "method":
			text = &v.Method
		case "interval_seconds":
			number = &v.IntervalSeconds
		case "timeout_seconds":
			number = &v.TimeoutSeconds
		case "expected_status":
			number = &v.ExpectedStatus
		default:
			return v, false
		}
		if text != nil {
			var s string
			if raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
				return v, false
			}
			*text = &s
		} else {
			s := string(raw)
			n, e := strconv.ParseInt(s, 10, 64)
			if e != nil || strconv.FormatInt(n, 10) != s {
				return v, false
			}
			*number = &n
		}
	}
	if len(seen) == 0 || create && (v.Name == nil || v.URL == nil) {
		return v, false
	}
	return clean(v), true
}
func clean(v Input) Input {
	if v.Name != nil {
		s := strings.Trim(*v.Name, " \t\r\n")
		v.Name = &s
	}
	if v.URL != nil {
		if s, ok := canonicalURL(*v.URL); ok {
			v.URL = &s
		}
	}
	return v
}
func defaults(v Input) Input {
	method, interval, timeout, status := "GET", int64(60), int64(10), int64(200)
	if v.Method == nil {
		v.Method = &method
	}
	if v.IntervalSeconds == nil {
		v.IntervalSeconds = &interval
	}
	if v.TimeoutSeconds == nil {
		v.TimeoutSeconds = &timeout
	}
	if v.ExpectedStatus == nil {
		v.ExpectedStatus = &status
	}
	return v
}
func validate(v Input) map[string]string {
	fields := map[string]string{}
	if v.Name != nil {
		s := *v.Name
		switch {
		case s == "":
			fields["name"] = "required"
		case utf8.RuneCountInString(s) > 100 || len(s) > 400:
			fields["name"] = "invalid_length"
		default:
			if !utf8.ValidString(s) || strings.TrimSpace(s) != s {
				fields["name"] = "invalid_format"
			}
			for _, r := range s {
				if unicode.Is(unicode.Cc, r) {
					fields["name"] = "invalid_format"
				}
			}
		}
	}
	if v.URL != nil {
		switch {
		case *v.URL == "":
			fields["url"] = "required"
		case len(*v.URL) > 2048:
			fields["url"] = "invalid_length"
		default:
			if _, ok := canonicalURL(*v.URL); !ok {
				fields["url"] = "invalid_format"
			}
		}
	}
	if v.Method != nil && *v.Method != "GET" {
		fields["method"] = "invalid_value"
	}
	if v.IntervalSeconds != nil {
		switch *v.IntervalSeconds {
		case 60, 300, 900, 1800, 3600:
		default:
			fields["interval_seconds"] = "invalid_value"
		}
	}
	if v.TimeoutSeconds != nil && (*v.TimeoutSeconds < 1 || *v.TimeoutSeconds > 30) {
		fields["timeout_seconds"] = "invalid_value"
	}
	if v.ExpectedStatus != nil && (*v.ExpectedStatus < 200 || *v.ExpectedStatus > 599) {
		fields["expected_status"] = "invalid_value"
	}
	if v.TimeoutSeconds != nil && v.IntervalSeconds != nil && fields["timeout_seconds"] == "" && fields["interval_seconds"] == "" && *v.TimeoutSeconds >= *v.IntervalSeconds {
		fields["timeout_seconds"] = "must_be_less_than_interval"
	}
	return fields
}
func empty(v Input) bool {
	return v.Name == nil && v.URL == nil && v.Method == nil && v.IntervalSeconds == nil && v.TimeoutSeconds == nil && v.ExpectedStatus == nil
}
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range []byte(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func selector(s string) any {
	if validUUID(s) {
		return s
	}
	return nil
}
func selectors(raw string, list bool) (int, string, bool) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return 0, "", false
	}
	limit, after := 50, ""
	for k, v := range q {
		if !list || len(v) != 1 {
			return 0, "", false
		}
		switch k {
		case "limit":
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != v[0] {
				return 0, "", false
			}
			limit = n
		case "after":
			if !validUUID(v[0]) {
				return 0, "", false
			}
			after = v[0]
		default:
			return 0, "", false
		}
	}
	return limit, after, true
}
func ETag(p Public) string { return `"monitor:` + p.ID + `:` + p.Version + `"` }
func condition(tag string) (string, int64, bool) {
	if len(tag) > 96 || !strings.HasPrefix(tag, `"monitor:`) || !strings.HasSuffix(tag, `"`) {
		return "", 0, false
	}
	parts := strings.Split(tag[9:len(tag)-1], ":")
	if len(parts) != 2 || !validUUID(parts[0]) {
		return "", 0, false
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	return parts[0], n, err == nil && n > 0 && strconv.FormatInt(n, 10) == parts[1]
}
