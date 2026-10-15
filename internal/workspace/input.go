package workspace

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rjgczp/devpilot/internal/credentials"
)

func parseName(body []byte) (string, bool) {
	if !credentials.ValidJSON(body) {
		return "", false
	}
	d := json.NewDecoder(strings.NewReader(string(body)))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') || !d.More() {
		return "", false
	}
	tok, err = d.Token()
	if err != nil || tok != "name" {
		return "", false
	}
	var raw json.RawMessage
	if d.Decode(&raw) != nil || len(raw) == 0 || raw[0] != '"' || d.More() {
		return "", false
	}
	var name string
	if json.Unmarshal(raw, &name) != nil {
		return "", false
	}
	return strings.Trim(name, " \t\r\n"), true
}

func nameReason(name string) string {
	if name == "" {
		return "required"
	}
	if utf8.RuneCountInString(name) > 100 || len(name) > 400 {
		return "invalid_length"
	}
	if strings.TrimSpace(name) != name {
		return "invalid_format"
	}
	for _, r := range name {
		if unicode.Is(unicode.Cc, r) {
			return "invalid_format"
		}
	}
	return ""
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

func selectors(raw string, list bool) (int, string, bool) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return 0, "", false
	}
	limit, after := 50, ""
	for key, v := range q {
		if !list || len(v) != 1 {
			return 0, "", false
		}
		switch key {
		case "limit":
			n, err := strconv.Atoi(v[0])
			if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != v[0] {
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
