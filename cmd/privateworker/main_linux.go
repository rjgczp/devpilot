//go:build privateworker && linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/rjgczp/devpilot/internal/storagecontrol"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rjgczp/devpilot/internal/workerruntime"
)

func run() int {
	flags := flag.NewFlagSet("privateworker", flag.ContinueOnError)
	file := flags.String("database-url-file", "", "restricted Worker DSN secret file")
	inc := flags.String("incarnation", "", "offline assigned Worker UUID")
	private := flags.Bool("private-isolated", false, "require separately approved private PG admission and inherited Supervisor IPC")
	catalog := flags.String("catalog-file", "", "restricted operator target catalog file")
	storage := flags.Bool("storage-watchdog", false, "require fresh monotonic admission on inherited read-only pipe5")
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

	if *private {
		loop, close, e := workerruntime.OpenPrivate(ctx, cfg, *inc)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		defer close()
		if *storage {
			fd, err := storagecontrol.InheritedPipe(5, false)
			if err != nil {
				return 2
			}
			guard := storagecontrol.NewGuard(fd)
			defer guard.Close()
			until := time.Now().Add(3 * time.Second)
			for !guard.Alive() && time.Now().Before(until) && ctx.Err() == nil {
				time.Sleep(20 * time.Millisecond)
			}
			if !guard.Alive() {
				return 2
			}
			loop.RequireStorageGuard(guard.Alive)
		}
		if *check {
			fmt.Println(`{"event":"worker_checked","gate":"private-isolated"}`)
			return 0
		}
		sup, e := workerruntime.InheritedSupervisor()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		defer sup.Close()
		dir, e := workerruntime.TargetCatalogFile(*catalog)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		hello, e := sup.Call(ctx, workerruntime.Call{Op: "hello"})
		if e != nil || hello.Event != "supervisor_ready" {
			fmt.Fprintln(os.Stderr, workerruntime.ErrBoundary)
			return 2
		}
		fmt.Println(`{"event":"worker_ready","gate":"private-isolated","execution_available":true}`)
		e = loop.Run(ctx, sup, dir, func(event, run string) {
			b, _ := json.Marshal(map[string]string{"event": event, "run_id": run})
			fmt.Println(string(b))
		})
		if e != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		return 0
	}
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
