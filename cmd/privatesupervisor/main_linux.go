//go:build privateworker && privateprobe && linux

package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rjgczp/devpilot/internal/supervisorruntime"
	"github.com/rjgczp/devpilot/internal/workerruntime"
	"os"
	"os/signal"
	"syscall"
)

func run() int {
	f := flag.NewFlagSet("privatesupervisor", flag.ContinueOnError)
	active := f.Bool("run", false, "opt-in control service; does not open a Gate")
	role := f.String("role", "supervisor", "supervisor or controller; separate LOGIN required")
	file := f.String("database-url-file", "", "own restricted LOGIN file")
	inc := f.String("incarnation", "", "offline registered incarnation")
	worker := f.String("worker-incarnation", "", "bound Worker incarnation, no Worker credential")
	pub := f.String("stop-public-key-file", "", "independently pinned public key only")
	if f.Parse(os.Args[1:]) != nil || f.NArg() != 0 {
		return 2
	}
	if !*active {
		fmt.Println(`{"event":"private_supervisor_closed"}`)
		return 0
	}
	if *role != "supervisor" && *role != "controller" || workerruntime.CheckControlBoundary() != nil {
		return 2
	}
	cfg, e := workerruntime.DatabaseConfig(*file, *inc)
	if e != nil {
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, e := supervisorruntime.OpenRole(ctx, cfg, "devpilot_scheduler_"+*role)
	if e != nil {
		return 2
	}
	defer db.Close()
	peer, e := supervisorruntime.InheritedPeer(3, 4)
	if e != nil {
		return 2
	}
	if *role == "controller" {
		other, e := supervisorruntime.InheritedPeer(5, 6)
		if e != nil {
			return 2
		}
		recovery, e := supervisorruntime.InheritedPeer(7, 8)
		if e != nil {
			return 2
		}
		c := supervisorruntime.Controller{DB: db, Incarnation: *inc}
		e = c.Run(ctx, []*supervisorruntime.Peer{peer, other, recovery})
	} else {
		raw, err := os.ReadFile(*pub)
		if err != nil || len(raw) > 4096 {
			return 2
		}
		kernel, err := supervisorruntime.InheritedPeer(5, 6)
		if err != nil {
			return 2
		}
		defer kernel.Close()
		control, err := supervisorruntime.InheritedPeer(7, 8)
		if err != nil {
			return 2
		}
		defer control.Close()
		s := supervisorruntime.Supervisor{DB: db, Incarnation: *inc, WorkerIncarnation: *worker, PublicKey: raw, Kernel: kernel, Controller: supervisorruntime.ControlClient{Peer: control}}
		defer s.Lost()
		e = supervisorruntime.Serve(ctx, peer, s.Handle)
	}
	if e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "private control service stopped; no inferred physical release")
		return 2
	}
	return 0
}
func main() { os.Exit(run()) }
