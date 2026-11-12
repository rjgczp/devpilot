//go:build privateworker && linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rjgczp/devpilot/internal/workerruntime"
)

func run() int {
	flags := flag.NewFlagSet("privateworker", flag.ContinueOnError)
	file := flags.String("database-url-file", "", "restricted Worker DSN secret file")
	inc := flags.String("incarnation", "", "offline assigned Worker UUID")
	check := flags.Bool("check", false, "validate the closed control boundary and exit")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 {
		return 2
	}
	if err := workerruntime.CheckControlBoundary(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := workerruntime.DatabaseConfig(*file, *inc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, close, err := workerruntime.OpenClosed(ctx, cfg, *inc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer close()
	fmt.Println(`{"event":"worker_ready","gate":"closed","execution_available":false}`)
	if !*check {
		<-ctx.Done()
		fmt.Println(`{"event":"worker_stopped","gate":"closed","physical_release":false}`)
	}
	return 0
}
func main() { os.Exit(run()) }
