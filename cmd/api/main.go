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
	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/health"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/registration"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
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
	healthRoutes := health.Handler{
		Checker:  pool,
		Timeout:  cfg.ReadinessTimeout,
		Draining: &draining,
		Logger:   logger,
	}.Routes()
	passwords := password.New()
	budget := password.NewBudget()
	users := user.Store{Pool: pool}
	sessions := session.Store{Pool: pool}
	authentication := auth.New(users, sessions, passwords, session.NewTokens(), budget, cfg.Auth, logger)
	handler := app.NewRoutes(healthRoutes, registration.New(users, passwords, logger, budget), authentication, workspace.New(workspace.Store{Pool: pool}, cfg.Auth, logger), project.New(project.Store{Pool: pool}, cfg.Auth, logger))
	cleanup := session.NewWorker(sessions, logger)
	runner := app.Runner{
		Server:          app.NewServer(cfg.HTTPAddr, handler),
		Workers:         []func(context.Context){cleanup.Run},
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
