//go:build privateworker && linux

package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rjgczp/devpilot/internal/retentioncontrol"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/storagecontrol"
	"github.com/rjgczp/devpilot/internal/workerruntime"
	"os"
	"os/signal"
	"syscall"
)

func run() int {
	f := flag.NewFlagSet("retentioncontroller", flag.ContinueOnError)
	active := f.Bool("run", false, "bounded Migration10 history maintenance only")
	dsn := f.String("database-url-file", "", "existing Controller LOGIN file")
	inc := f.String("incarnation", "", "registered Controller incarnation")
	if f.Parse(os.Args[1:]) != nil || f.NArg() != 0 {
		return 2
	}
	if !*active {
		fmt.Println(`{"event":"retention_controller_closed"}`)
		return 0
	}
	if workerruntime.CheckControlBoundary() != nil {
		return 2
	}
	cfg, e := workerruntime.DatabaseConfig(*dsn, *inc)
	if e != nil {
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, e := storagecontrol.OpenControl(ctx, cfg, "devpilot_scheduler_controller")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	defer db.Close()
	e = retentioncontrol.Run(ctx, scheduler.Store{DB: db, Incarnation: *inc})
	if e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "retention maintenance unavailable")
		return 2
	}
	return 0
}
func main() { os.Exit(run()) }
