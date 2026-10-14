package user

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err, want error
	}{
		{"email", &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key", Detail: "secret"}, ErrConflict},
		{"primary", &pgconn.PgError{Code: "23505", ConstraintName: "users_pkey"}, ErrInternal},
		{"check", &pgconn.PgError{Code: "23514"}, ErrInternal},
		{"schema", &pgconn.PgError{Code: "42P01"}, ErrSchema},
		{"connect", &net.OpError{Op: "dial", Err: errors.New("secret")}, ErrUnavailable},
		{"eof", io.EOF, ErrUnavailable}, {"deadline", context.DeadlineExceeded, ErrTimeout},
		{"canceled", context.Canceled, ErrCanceled},
		{"statement", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}, ErrTimeout},
		{"unknown-cancel", &pgconn.PgError{Code: "57014", Message: "administrator canceled"}, ErrInternal},
		{"shutdown", &pgconn.PgError{Code: "57P01"}, ErrUnavailable}, {"unknown", errors.New("secret"), ErrInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(context.Background(), fmt.Errorf("wrapped: %w", tc.err)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Classify(ctx, &pgconn.PgError{Code: "57014"}) != ErrCanceled {
		t.Fatal("client cancellation takes precedence")
	}
}
