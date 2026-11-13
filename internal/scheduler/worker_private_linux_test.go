//go:build privateworker && linux

package scheduler

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type privateGuardDB struct {
	forbiddenWorkerDB
	err   error
	calls int
}
type privateBoolRow struct{ err error }

func (r privateBoolRow) Scan(out ...any) error {
	if r.err != nil {
		return r.err
	}
	*(out[0].(*bool)) = false
	return nil
}
func (d *privateGuardDB) QueryRow(ctx context.Context, sql string, a ...any) pgx.Row {
	if sql != `SELECT scheduler_private.private_worker_ready($1::uuid)` || len(a) != 1 || a[0] != "self" {
		panic("private gate bypass")
	}
	d.calls++
	return privateBoolRow{d.err}
}
func TestPrivateWorkerNeverUsesFixtureOrMissingAdmission(t *testing.T) {
	for _, e := range []error{nil, errors.New("missing candidate or permission")} {
		d := &privateGuardDB{err: e}
		w := NewPrivateWorker(Store{DB: d, Incarnation: "self"})
		if w.PrivateReady(context.Background()) {
			t.Fatal("unapproved mode opened")
		}
		if _, e := w.Candidates(context.Background(), ""); !errors.Is(e, ErrClosed) {
			t.Fatal(e)
		}
		if _, e := w.Claim(context.Background(), Hint{}); !errors.Is(e, ErrClosed) {
			t.Fatal(e)
		}
		if _, e := w.Start(context.Background(), Hint{}, Lease{}); !errors.Is(e, ErrClosed) {
			t.Fatal(e)
		}
		if d.calls != 4 {
			t.Fatal("admission was cached")
		}
	}
}
