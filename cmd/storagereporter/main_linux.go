//go:build privateworker && linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/rjgczp/devpilot/internal/storagecontrol"
	"github.com/rjgczp/devpilot/internal/workerruntime"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func run() int {
	f := flag.NewFlagSet("storagereporter", flag.ContinueOnError)
	active := f.Bool("run", false, "explicit sampler startup; never opens deployment Gate")
	watchdog := f.Bool("watchdog", false, "independent deny-only watchdog, input3 / local guard outputs4,5")
	identify := f.String("identify", "", "offline metadata only, no database registration")
	config := f.String("bindings-file", "", "operator device binding JSON")
	dsn := f.String("database-url-file", "", "dedicated Reporter LOGIN file")
	inc := f.String("incarnation", "", "offline registered Reporter incarnation")
	if f.Parse(os.Args[1:]) != nil || f.NArg() != 0 {
		return 2
	}
	if *identify != "" {
		b, e := storagecontrol.Identify(*identify)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		json.NewEncoder(os.Stdout).Encode(b)
		return 0
	}
	if !*active && !*watchdog {
		fmt.Println(`{"event":"storage_reporter_closed"}`)
		return 0
	}
	if *active && *watchdog || workerruntime.CheckControlBoundary() != nil {
		return 2
	}
	cfg, e := workerruntime.DatabaseConfig(*dsn, *inc)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, e := storagecontrol.OpenControl(ctx, cfg, "devpilot_storage_reporter")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	defer db.Close()
	client := storagecontrol.Client{DB: db, Incarnation: *inc}
	if *watchdog {
		in, e := storagecontrol.InheritedPipe(3, false)
		if e != nil {
			return 2
		}
		defer in.Close()
		a, e := storagecontrol.InheritedPipe(4, true)
		if e != nil {
			return 2
		}
		b, e := storagecontrol.InheritedPipe(5, true)
		if e != nil {
			a.Close()
			return 2
		}
		e = storagecontrol.Watch(ctx, client, in, []*os.File{a, b})
	} else {
		file, err := os.Open(*config)
		if err != nil {
			return 2
		}
		raw, err := io.ReadAll(io.LimitReader(file, 8193))
		file.Close()
		if err != nil || len(raw) > 8192 {
			return 2
		}
		var bindings []storagecontrol.Binding
		if json.Unmarshal(raw, &bindings) != nil {
			return 2
		}
		sampler, err := storagecontrol.Open(bindings)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer sampler.Close()
		out, err := storagecontrol.InheritedPipe(3, true)
		if err != nil {
			return 2
		}
		defer out.Close()
		e = storagecontrol.Run(ctx, client, sampler, out)
	}
	if e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, storagecontrol.ErrReport)
		return 2
	}
	return 0
}
func main() { os.Exit(run()) }
