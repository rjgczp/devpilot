// Package project is the sole supported writer of project attributes. Other
// business modules must use Store.Update, retaining its authority/version gate.
package project

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

type Input struct{ Name, Description *string }

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
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return v, false
		}
		var dest **string
		switch tok {
		case "name":
			dest = &v.Name
		case "description":
			dest = &v.Description
		default:
			return v, false
		}
		if *dest != nil {
			return v, false
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil || len(raw) == 0 || raw[0] != '"' {
			return v, false
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return v, false
		}
		*dest = &s
	}
	if create && v.Name == nil || v.Name == nil && v.Description == nil {
		return v, false
	}
	if v.Name != nil {
		s := strings.Trim(*v.Name, " \t\r\n")
		v.Name = &s
	}
	return v, true
}

func validate(v Input) map[string]string {
	fields := map[string]string{}
	if v.Name != nil {
		if reason := textReason(*v.Name, true); reason != "" {
			fields["name"] = reason
		}
	}
	if v.Description != nil {
		if reason := textReason(*v.Description, false); reason != "" {
			fields["description"] = reason
		}
	}
	return fields
}
func textReason(s string, name bool) string {
	n, b := 2000, 8000
	if name {
		n, b = 100, 400
		if s == "" {
			return "required"
		}
	}
	if !utf8.ValidString(s) {
		return "invalid_format"
	}
	if utf8.RuneCountInString(s) > n || len(s) > b {
		return "invalid_length"
	}
	if name && strings.TrimSpace(s) != s {
		return "invalid_format"
	}
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) && (name || r != '\t' && r != '\n') {
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
func ETag(p Public) string { return `"project:` + p.ID + `:` + p.Version + `"` }
func condition(tag string) (string, int64, bool) {
	if len(tag) > 96 || !strings.HasPrefix(tag, `"project:`) || !strings.HasSuffix(tag, `"`) {
		return "", 0, false
	}
	parts := strings.Split(tag[9:len(tag)-1], ":")
	if len(parts) != 2 || !validUUID(parts[0]) {
		return "", 0, false
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	return parts[0], n, err == nil && n > 0 && strconv.FormatInt(n, 10) == parts[1]
}
