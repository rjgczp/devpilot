// Package session owns digest-only PostgreSQL sessions and bounded cleanup.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
)

var ErrToken = errors.New("invalid session token")

type Digest [sha256.Size]byte
type Tokens struct{ random io.Reader }

func NewTokens() *Tokens { return &Tokens{random: rand.Reader} }

func (t *Tokens) New() (string, Digest, error) {
	var secret [32]byte
	if _, err := io.ReadFull(t.random, secret[:]); err != nil {
		return "", Digest{}, errors.New("session randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(secret[:]), sha256.Sum256(secret[:]), nil
}

func HashToken(token string) (Digest, error) {
	var secret [32]byte
	codec := base64.RawURLEncoding.Strict()
	if len(token) != 43 {
		return Digest{}, ErrToken
	}
	n, err := codec.Decode(secret[:], []byte(token))
	if err != nil || n != len(secret) || codec.EncodeToString(secret[:]) != token {
		return Digest{}, ErrToken
	}
	return sha256.Sum256(secret[:]), nil
}

// Count raw same-name pairs too: net/http can silently drop malformed Cookies,
// which must not turn an ambiguous header into an accepted single credential.
func CookieDigest(r *http.Request, name string) (Digest, bool) {
	count, token := 0, ""
	for _, header := range r.Header.Values("Cookie") {
		for _, pair := range strings.Split(header, ";") {
			key, value, hasValue := strings.Cut(strings.TrimSpace(pair), "=")
			if strings.TrimSpace(key) == name {
				count++
				if !hasValue {
					return Digest{}, false
				}
				token = strings.TrimSpace(value)
			}
		}
	}
	if count != 1 {
		return Digest{}, false
	}
	digest, err := HashToken(token)
	return digest, err == nil
}
