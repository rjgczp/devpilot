// Package testdb guards the disposable database used by integration tests.
package testdb

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const Name = "devpilot_registration_test"

func ValidateURL(testURL, applicationURL string) error {
	u, err := url.Parse(testURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path != "/"+Name {
		return errors.New("TEST_DATABASE_URL must name the isolated registration test database")
	}
	if applicationURL != "" {
		app, err := url.Parse(applicationURL)
		if err != nil || testURL == applicationURL || strings.TrimPrefix(app.Path, "/") == Name {
			return errors.New("test database must be separate from DATABASE_URL")
		}
	}
	return nil
}

func Open(ctx context.Context, testURL, applicationURL string) (*pgxpool.Pool, error) {
	if err := ValidateURL(testURL, applicationURL); err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		return nil, errors.New("test database configuration invalid")
	}
	var name string
	var version int
	err = pool.QueryRow(ctx, `SELECT current_database(), current_setting('server_version_num')::integer`).Scan(&name, &version)
	if err != nil || name != Name || version < 180000 || version >= 190000 {
		pool.Close()
		return nil, errors.New("isolated PostgreSQL 18 test database unavailable")
	}
	return pool, nil
}
