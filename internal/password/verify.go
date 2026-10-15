package password

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

var ErrInvalidHash = errors.New("invalid password hash")

// DummyPHC was generated once with the approved policy and a fixed public salt.
// Its matching password is public test data, never an authenticating identity.
const DummyPHC = "$argon2id$v=19$m=65536,t=3,p=1$ZGV2cGlsb3QtZHVtbXktdg$dQ7E0WBNFjVaRlZ4p13poWYvdDqeWvmcXsM4+EnTH6Q"

// parsed is deliberately fixed-size: untrusted PHC parameters never control
// allocation or enter IDKey. A new accepted policy requires a reviewed change.
type parsed struct {
	salt [SaltLength]byte
	key  [KeyLength]byte
}

func parse(encoded string) (parsed, error) {
	var value parsed
	if len(encoded) == 0 || len(encoded) > 1024 {
		return value, ErrInvalidHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=1" {
		return value, ErrInvalidHash
	}
	if !decode(parts[4], value.salt[:]) || !decode(parts[5], value.key[:]) {
		return parsed{}, ErrInvalidHash
	}
	return value, nil
}

func decode(encoded string, target []byte) bool {
	codec := base64.RawStdEncoding.Strict()
	if len(encoded) != codec.EncodedLen(len(target)) {
		return false
	}
	n, err := codec.Decode(target, []byte(encoded))
	return err == nil && n == len(target) && codec.EncodeToString(target) == encoded
}

// Valid is a cheap policy check, used before selecting real or dummy work.
func Valid(encoded string) bool { _, err := parse(encoded); return err == nil }

func (h *Hasher) Verify(secret, encoded string) (bool, error) {
	p, err := parse(encoded)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(secret), p.salt[:], Iterations, Memory, Parallelism, KeyLength)
	return subtle.ConstantTimeCompare(key, p.key[:]) == 1, nil
}
