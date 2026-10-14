package registration

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"unicode/utf8"
)

const MaxBodyBytes = 4096

type input struct{ Email, Password string }

var errJSON = errors.New("invalid registration JSON")

func parseInput(body []byte) (input, error) {
	if !utf8.Valid(body) || !json.Valid(body) || !validEscapes(body) {
		return input{}, errJSON
	}
	d := json.NewDecoder(bytes.NewReader(body))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return input{}, errJSON
	}
	values := map[string]string{}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return input{}, errJSON
		}
		key, ok := tok.(string)
		if !ok || (key != "email" && key != "password") {
			return input{}, errJSON
		}
		if _, exists := values[key]; exists {
			return input{}, errJSON
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil || len(raw) == 0 || raw[0] != '"' {
			return input{}, errJSON
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return input{}, errJSON
		}
		values[key] = value
	}
	if len(values) != 2 {
		return input{}, errJSON
	}
	return input{Email: values["email"], Password: values["password"]}, nil
}

// encoding/json accepts unpaired surrogates by replacing them with U+FFFD.
// Validate the original string escapes before decoding. A literal U+FFFD is valid.
// json.Valid has already checked JSON grammar and escape widths.
func validEscapes(b []byte) bool {
	inString := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || b[i] != '\\' {
			continue
		}
		i++
		if b[i] != 'u' {
			continue
		}
		u := hex4(b[i+1 : i+5])
		i += 4
		if u >= 0xDC00 && u <= 0xDFFF {
			return false
		}
		if u >= 0xD800 && u <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low := hex4(b[i+3 : i+7])
			if low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}

func hex4(b []byte) uint16 {
	var value uint16
	for _, c := range b {
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			value += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			value += uint16(c - 'A' + 10)
		default:
			return 0 // Only reached for a malformed trailing pair; reject it.
		}
	}
	return value
}

func validate(in *input) map[string]string {
	in.Email = strings.Trim(in.Email, " \t\r\n")
	fields := map[string]string{}
	if in.Email == "" {
		fields["email"] = "required"
	} else if emailTooLong(in.Email) {
		fields["email"] = "invalid_length"
	} else if !validEmail(in.Email) {
		fields["email"] = "invalid_format"
	}
	in.Email = strings.ToLower(in.Email)
	n := utf8.RuneCountInString(in.Password)
	if in.Password == "" {
		fields["password"] = "required"
	} else if n < 15 || n > 128 || len(in.Password) > 512 {
		fields["password"] = "invalid_length"
	}
	return fields
}

func emailTooLong(email string) bool {
	if len(email) > 254 {
		return true
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) > 64 {
		return true
	}
	for _, label := range strings.Split(parts[1], ".") {
		if len(label) > 63 {
			return true
		}
	}
	return false
}

func validEmail(email string) bool {
	for _, c := range []byte(email) {
		if c > 127 {
			return false
		}
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 64 {
		return false
	}
	local := parts[0]
	if local[0] == '.' || local[len(local)-1] == '.' || strings.Contains(local, "..") {
		return false
	}
	for _, c := range []byte(local) {
		if !alnum(c) && !strings.ContainsRune(".!#$%&'*+-/=?^_`{|}~", rune(c)) {
			return false
		}
	}
	domain := parts[1]
	if net.ParseIP(domain) != nil {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || !alnum(label[0]) || !alnum(label[len(label)-1]) {
			return false
		}
		for _, c := range []byte(label) {
			if !alnum(c) && c != '-' {
				return false
			}
		}
	}
	return true
}

func alnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }
