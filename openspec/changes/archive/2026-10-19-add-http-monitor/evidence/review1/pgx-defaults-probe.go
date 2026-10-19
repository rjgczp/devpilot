//go:build ignore

// Diagnostic only: ParseConfig computes defaults without opening a DB connection.
package main

import (
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgconn"
)

func main() {
	fmt.Printf("pid=%d before pgconn.ParseConfig with explicit IP/user/database\n", os.Getpid())
	if _, err := pgconn.ParseConfig("postgres://devpilot_test:registration_test_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable"); err != nil {
		panic(err)
	}
	fmt.Printf("pid=%d after ParseConfig; no Connect invoked\n", os.Getpid())
}
