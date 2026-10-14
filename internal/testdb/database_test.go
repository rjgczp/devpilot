package testdb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDatabaseGuard(t *testing.T) {
	valid := "postgres://test:sentinel@127.0.0.1:55432/" + Name
	for _, tc := range []struct {
		test, app string
		ok        bool
	}{
		{valid, "", true}, {"", "", false}, {"postgres://test:sentinel@localhost/devpilot", "", false},
		{"postgres://test:sentinel@localhost/other", "", false}, {valid, valid, false},
		{valid, "postgres://different@other-host/" + Name, false}, {valid, "postgres://test@localhost/devpilot", true},
		{"invalid://sentinel/" + Name, "", false},
	} {
		err := ValidateURL(tc.test, tc.app)
		if (err == nil) != tc.ok {
			t.Fatalf("unexpected guard result: %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "sentinel") {
			t.Fatal("guard leaked credential")
		}
	}
}

func TestUnavailableDatabaseIsFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pool, err := Open(ctx, "postgres://test:sentinel@127.0.0.1:1/"+Name, "")
	if err == nil || pool != nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("unreachable database must fail safely")
	}
}
