package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultHTTPAddr         = ":8080"
	defaultReadinessTimeout = 2 * time.Second
	defaultShutdownTimeout  = 10 * time.Second
)

type Config struct {
	Auth             AuthConfig
	HTTPAddr         string
	Database         *pgxpool.Config
	LogLevel         slog.Level
	ReadinessTimeout time.Duration
	ShutdownTimeout  time.Duration
}

// Load reads the process environment. It deliberately does not read .env files.
func Load(getenv func(string) string) (Config, error) {
	var cfg Config

	cfg.HTTPAddr = valueOrDefault(getenv("HTTP_ADDR"), defaultHTTPAddr)
	if err := validateAddr(cfg.HTTPAddr); err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR: %w", err)
	}

	var err error
	cfg.LogLevel, err = parseLogLevel(valueOrDefault(getenv("LOG_LEVEL"), "info"))
	if err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	cfg.ReadinessTimeout, err = parsePositiveDuration(getenv("READINESS_TIMEOUT"), defaultReadinessTimeout)
	if err != nil {
		return Config{}, fmt.Errorf("READINESS_TIMEOUT: %w", err)
	}
	cfg.ShutdownTimeout, err = parsePositiveDuration(getenv("SHUTDOWN_TIMEOUT"), defaultShutdownTimeout)
	if err != nil {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
	}

	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return Config{}, fmt.Errorf("DATABASE_URL: required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" {
		return Config{}, fmt.Errorf("DATABASE_URL: invalid PostgreSQL URL")
	}
	cfg.Database, err = pgxpool.ParseConfig(dsn)
	if err != nil {
		return Config{}, fmt.Errorf("DATABASE_URL: invalid PostgreSQL URL")
	}
	cfg.Auth, err = loadAuth(getenv, cfg.HTTPAddr)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func parsePositiveDuration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("must be a positive duration (for example 10s)")
	}
	return duration, nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("must be debug, info, warn, or error")
	}
}

func validateAddr(addr string) error {
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
