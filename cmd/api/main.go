package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/app"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/health"
)

func main() { os.Exit(run()) }

func run() int {
	logger := app.NewLogger(os.Stderr, slog.LevelInfo)
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("configuration invalid", "reason", err.Error())
		return 1
	}
	logger = app.NewLogger(os.Stderr, cfg.LogLevel)

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg.Database)
	if err != nil {
		logger.Error("database pool creation failed", "reason", "pool_configuration_error")
		return 1
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		pool.Close()
		logger.Error("server listen failed", "reason", "listen_error")
		return 1
	}
	defer listener.Close()

	var draining atomic.Bool
	handler := health.Handler{
		Checker:  pool,
		Timeout:  cfg.ReadinessTimeout,
		Draining: &draining,
		Logger:   logger,
	}.Routes()
	runner := app.Runner{
		Server:          app.NewServer(cfg.HTTPAddr, handler),
		Logger:          logger,
		Draining:        &draining,
		ShutdownTimeout: cfg.ShutdownTimeout,
		CloseResource: func() error {
			pool.Close()
			return nil
		},
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(signalContext, listener); err != nil {
		logger.Error("service failed", "reason", "runtime_error")
		return 1
	}
	return 0
}
