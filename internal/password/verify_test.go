package password

import (
	"strings"
	"testing"
)

func TestVerifyAndApprovedPolicy(t *testing.T) {
	h := New()
	secret := " exact 密码 e\u0301 🔐 phrase "
	encoded, err := h.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	if match, err := h.Verify(secret, encoded); err != nil || !match {
		t.Fatal("original password rejected")
	}
	if match, err := h.Verify(strings.TrimSpace(secret), encoded); err != nil || match {
		t.Fatal("password was normalized")
	}
	if match, err := h.Verify("different", encoded); err != nil || match {
		t.Fatal("wrong password accepted")
	}
	parts := strings.Split(encoded, "$")
	bad := []string{"", strings.Repeat("x", 1025), encoded + "$extra"}
	for _, header := range []string{"argon2i$v=19$m=65536,t=3,p=1", "argon2id$v=16$m=65536,t=3,p=1", "argon2id$v=19$m=0,t=3,p=1", "argon2id$v=19$m=65537,t=3,p=1", "argon2id$v=19$m=4294967296,t=3,p=1", "argon2id$v=19$m=65536,t=0,p=1", "argon2id$v=19$m=65536,t=4,p=1", "argon2id$v=19$m=65536,t=3,p=0", "argon2id$v=19$m=65536,t=3,p=256", "argon2id$v=19$m=065536,t=3,p=1", "argon2id$v=19$m=+65536,t=3,p=1", "argon2id$v=19$t=3,m=65536,p=1", "argon2id$v=19$m=65536,t=3,p=1,p=1"} {
		bad = append(bad, "$"+header+"$"+parts[4]+"$"+parts[5])
	}
	for _, salt := range []string{parts[4] + "=", parts[4] + "\n", parts[4][:21], strings.Repeat("-", 22), parts[4][:21] + "B"} {
		bad = append(bad, "$argon2id$v=19$m=65536,t=3,p=1$"+salt+"$"+parts[5])
	}
	for _, key := range []string{parts[5] + "=", parts[5] + "\r", parts[5][:42], strings.Repeat("_", 43), parts[5][:42] + "B"} {
		bad = append(bad, "$argon2id$v=19$m=65536,t=3,p=1$"+parts[4]+"$"+key)
	}
	for i, value := range bad {
		if Valid(value) {
			t.Fatalf("accepted invalid policy case %d", i)
		}
		if match, err := h.Verify(secret, value); err != ErrInvalidHash || match {
			t.Fatalf("invalid case %d reached verification", i)
		}
	}
}

func FuzzPHC(f *testing.F) {
	for _, seed := range []string{DummyPHC, "", "$argon2id$v=19$m=4294967295,t=3,p=255$AAAA$BBBB", strings.Repeat("x", 1025)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) { _, _ = parse(value) })
}

func TestDummyPHC(t *testing.T) {
	if !Valid(DummyPHC) {
		t.Fatal("dummy policy is invalid")
	}
	if match, err := New().Verify("devpilot dummy credential", DummyPHC); err != nil || !match {
		t.Fatal("dummy was not precomputed correctly")
	}
}

func TestBudgetNeverQueues(t *testing.T) {
	b := NewBudget()
	if !b.Acquire() || !b.Acquire() || b.Acquire() || b.Active() != 2 {
		t.Fatal("incorrect bound")
	}
	b.Release()
	if !b.Acquire() {
		t.Fatal("slot not reusable")
	}
	b.Release()
	b.Release()
	if b.Active() != 0 {
		t.Fatal("slot leaked")
	}
}
