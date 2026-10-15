// Package password hashes and verifies the approved Argon2id PHC policy.
package password

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	Memory      = 64 * 1024 // KiB, not bytes.
	Iterations  = 3
	Parallelism = 1
	SaltLength  = 16
	KeyLength   = 32
)

type Hasher struct{ random io.Reader }

func New() *Hasher { return &Hasher{random: rand.Reader} }

func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, SaltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", errors.New("password hashing unavailable")
	}
	key := argon2.IDKey([]byte(password), salt, Iterations, Memory, Parallelism, KeyLength)
	return "$argon2id$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
