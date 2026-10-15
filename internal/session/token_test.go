package session

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenCanonicalAndDigest(t *testing.T) {
	a, da, err := NewTokens().New()
	if err != nil {
		t.Fatal(err)
	}
	b, db, err := NewTokens().New()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 43 || a == b || da == db {
		t.Fatal("secret independence")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(a)
	if len(raw) != 32 || da != sha256.Sum256(raw) {
		t.Fatal("wrong digest representation")
	}
	got, err := HashToken(a)
	if err != nil || got != da {
		t.Fatal("token cannot verify")
	}
	for _, bad := range []string{"", a + "=", a + "\n", a[:42], a[:42] + "B", strings.Repeat("+", 43)} {
		if _, err := HashToken(bad); err != ErrToken {
			t.Fatal("noncanonical token accepted")
		}
	}
	for _, headers := range [][]string{{"s=" + a}, {"other=x; s=" + a}, {"s=" + a + "; s=" + a}, {"s=" + a, "s=bad\""}, {"s=\"" + a + "\""}, {""}} {
		r := httptest.NewRequest("GET", "/api/v1/auth/me?token="+a, nil)
		for _, h := range headers {
			r.Header.Add("Cookie", h)
		}
		_, valid := CookieDigest(r, "s")
		want := len(headers) == 1 && (headers[0] == "s="+a || headers[0] == "other=x; s="+a)
		if valid != want {
			t.Fatal("ambiguous Cookie accepted")
		}
	}
	if _, _, err := (&Tokens{random: strings.NewReader("short")}).New(); err == nil || strings.Contains(err.Error(), "short") {
		t.Fatal("RNG error not safe")
	}
}

func TestCookieDigestPreservesTokenBytes(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	wantDigest, err := HashToken(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		headers []string
		valid   bool
	}{
		{"canonical", []string{"s=" + token}, true},
		{"separator space", []string{"other=x; s=" + token}, true},
		{"separator tab", []string{"other=x;\ts=" + token}, true},
		{"separator mixed whitespace", []string{"other=x; \t s=" + token + "; other2=y"}, true},
		{"header leading whitespace", []string{" \ts=" + token}, true},
		{"separate headers", []string{"other=x", "s=" + token}, true},
		{"leading value space", []string{"s= " + token}, false},
		{"leading value tab", []string{"s=\t" + token}, false},
		{"trailing value space", []string{"s=" + token + " "}, false},
		{"trailing value tab", []string{"s=" + token + "\t"}, false},
		{"surrounding value whitespace", []string{"s= \t" + token + "\t "}, false},
		{"space before separator", []string{"s=" + token + " ; other=x"}, false},
		{"tab before separator", []string{"s=" + token + "\t; other=x"}, false},
		{"embedded space", []string{"s=" + token[:20] + " " + token[20:]}, false},
		{"embedded tab", []string{"s=" + token[:20] + "\t" + token[20:]}, false},
		{"unicode whitespace", []string{"s=\u00a0" + token + "\u00a0"}, false},
		{"duplicate malformed value", []string{"s=" + token, "s=\t" + token}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
			for _, header := range tc.headers {
				r.Header.Add("Cookie", header)
			}
			got, valid := CookieDigest(r, "s")
			if valid != tc.valid {
				t.Fatalf("valid = %v, want %v", valid, tc.valid)
			}
			if valid && got != wantDigest {
				t.Fatal("canonical token digest changed")
			}
		})
	}
}
