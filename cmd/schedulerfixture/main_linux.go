//go:build schedulerfixture && linux

// This command is an isolated test harness, not a deployable production worker.
package main

import (
	"fmt"
	"os"
)

func main() {
	// No environment setting can grant a network or execution capability.
	fmt.Fprintln(os.Stderr, "scheduler fixture requires a verified isolated supervisor")
	os.Exit(2)
}
