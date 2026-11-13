//go:build linux

package storagecontrol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealMetadataBindingAndUnavailable(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "database-private")
	if os.WriteFile(secret, []byte("not-a-metric"), 0600) != nil {
		t.Fatal("fixture")
	}
	b, e := Identify(dir)
	if e != nil {
		t.Fatal(e)
	}
	s, e := Open([]Binding{b})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	v, e := s.Collect()
	if e != nil || len(v) != 1 || v[0].Total <= 0 || v[0].Available <= 0 {
		t.Fatal(v, e)
	}
	bad := b
	bad.Device = "mismatched-device"
	if x, e := Open([]Binding{bad}); e == nil {
		x.Close()
		t.Fatal("unbound device accepted")
	}
	link := filepath.Join(t.TempDir(), "alias")
	if os.Symlink(dir, link) != nil {
		t.Fatal("fixture")
	}
	b.Path = link
	if x, e := Open([]Binding{b}); e == nil {
		x.Close()
		t.Fatal("symlink accepted")
	}
	s.Close()
	if _, e := s.Collect(); e == nil {
		t.Fatal("closed descriptor reused")
	}
}
