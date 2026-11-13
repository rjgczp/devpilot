//go:build privateworker && linux

// Package workerruntime is the closed private Worker composition root. It has
// no Probe launcher, listener, Supervisor key or network-management operation.
package workerruntime

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

var ErrConfiguration = errors.New("private worker configuration rejected")
var ErrBoundary = errors.New("private worker control boundary rejected")

// DatabaseConfig never accepts credentials from flags/environment. Error text
// contains neither the filename nor DSN. The process gets one restricted LOGIN.
func DatabaseConfig(file, incarnation string) (*pgxpool.Config, error) {
	var id pgtype.UUID
	if id.Scan(incarnation) != nil || !id.Valid {
		return nil, ErrConfiguration
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, ErrConfiguration
	}
	b, err := io.ReadAll(io.LimitReader(f, 8193))
	_ = f.Close()
	dsn := strings.TrimSpace(string(b))
	if err != nil || len(b) > 8192 || strings.ContainsAny(dsn, "\r\n") {
		return nil, ErrConfiguration
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.User == nil || u.User.Username() == "" {
		return nil, ErrConfiguration
	}
	if password, ok := u.User.Password(); !ok || password == "" {
		return nil, ErrConfiguration
	}
	for _, name := range []string{"postgres", "devpilot_migrator", "devpilot_private_app"} {
		if u.User.Username() == name {
			return nil, ErrConfiguration
		}
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.User != u.User.Username() {
		return nil, ErrConfiguration
	}
	if cfg.ConnConfig.RuntimeParams["options"] != "" {
		return nil, ErrConfiguration
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 2 * time.Second
	for k, v := range map[string]string{"statement_timeout": "2s", "transaction_timeout": "2s", "lock_timeout": "250ms"} {
		cfg.ConnConfig.RuntimeParams[k] = v
	}
	return cfg, nil
}

func CleanEnvironment(values []string) bool {
	for _, value := range values {
		key, v, _ := strings.Cut(value, "=")
		if v == "" {
			continue
		}
		for _, prefix := range []string{"AWS_", "AZURE_", "GOOGLE_", "GCLOUD_", "CLOUDSDK_"} {
			if strings.HasPrefix(key, prefix) {
				return false
			}
		}
		switch key {
		case "DATABASE_URL", "TEST_DATABASE_URL", "PGPASSWORD", "PGPASSFILE", "PGSERVICE", "PGSERVICEFILE", "OPENAI_API_KEY", "GITHUB_TOKEN":
			return false
		}
	}
	return true
}

// defaultRoutes checks actual control namespace routes, not an environment
// assertion. Internal PG connectivity needs neither a usable v4 nor v6 default.
func defaultRoutes(v4, v6 string) bool {
	lines := strings.Split(strings.TrimSpace(v4), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Iface") {
		return true
	}
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) < 8 {
			return true
		}
		flags, e := strconv.ParseUint(f[3], 16, 32)
		if e != nil {
			return true
		}
		if f[1] == "00000000" && f[7] == "00000000" && flags&1 != 0 {
			return true
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(v6), "\n") {
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 10 {
			return true
		}
		flags, e := strconv.ParseUint(f[8], 16, 32)
		if e != nil {
			return true
		}
		if f[0] == strings.Repeat("0", 32) && f[1] == "00" && flags&1 != 0 {
			return true
		}
	}
	return false
}

func CheckControlBoundary() error {
	if os.Getuid() == 0 || !CleanEnvironment(os.Environ()) {
		return ErrBoundary
	}
	b, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return ErrBoundary
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[k] = strings.TrimSpace(v)
		}
	}
	if fields["NoNewPrivs"] != "1" {
		return ErrBoundary
	}
	for _, k := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		if fields[k] != "0000000000000000" {
			return ErrBoundary
		}
	}
	v4, e4 := os.ReadFile("/proc/net/route")
	v6, e6 := os.ReadFile("/proc/net/ipv6_route")
	if e4 != nil || e6 != nil || defaultRoutes(string(v4), string(v6)) {
		return ErrBoundary
	}
	return nil
}

// OpenClosed verifies effective authority using the actual LOGIN. It does not
// SET ROLE, register an incarnation, issue a Claim or inspect existing Run rows.
func openPool(ctx context.Context, cfg *pgxpool.Config, incarnation string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var safe bool
	err = pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int/10000=18
 AND current_setting('statement_timeout')='2s' AND current_setting('transaction_timeout')='2s' AND current_setting('lock_timeout')='250ms'
 AND session_user=current_user
 AND NOT(SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=session_user)
 AND pg_has_role(session_user,'devpilot_scheduler_worker','MEMBER')
 AND NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname<>session_user AND rolname<>'devpilot_scheduler_worker' AND pg_has_role(session_user,oid,'MEMBER'))
 AND NOT has_database_privilege(session_user,current_database(),'CREATE')
 AND NOT has_database_privilege(session_user,current_database(),'TEMP')
 AND NOT has_schema_privilege(session_user,'public','CREATE')
 AND NOT has_schema_privilege(session_user,'scheduler_private','CREATE')
 AND NOT has_table_privilege(session_user,'public.sessions','SELECT')
 AND NOT has_column_privilege(session_user,'public.users','password_hash','SELECT')
 AND NOT has_any_column_privilege(session_user,'public.monitor_runs','UPDATE')
 AND NOT has_any_column_privilege(session_user,'public.scheduler_control','UPDATE')`).Scan(&safe)
	if err != nil || !safe {
		pool.Close()
		return nil, ErrBoundary
	}
	return pool, nil
}

func OpenClosed(ctx context.Context, cfg *pgxpool.Config, incarnation string) (*scheduler.Worker, func(), error) {
	pool, err := openPool(ctx, cfg, incarnation)
	if err != nil {
		return nil, func() {}, err
	}
	w := scheduler.NewClosedWorker(scheduler.Store{DB: pool, Incarnation: incarnation})
	if _, err = w.Candidates(ctx, ""); !errors.Is(err, scheduler.ErrClosed) {
		pool.Close()
		return nil, func() {}, ErrBoundary
	}
	return w, pool.Close, nil
}
