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
