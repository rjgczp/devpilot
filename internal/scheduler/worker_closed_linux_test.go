//go:build (schedulerfixture || privateworker) && linux

package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type forbiddenWorkerDB struct{}

func (forbiddenWorkerDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("closed Worker attempted a write")
}
func (forbiddenWorkerDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("closed Worker attempted candidate discovery")
}

func TestClosedWorkerCannotAcquireOrReconstructStart(t *testing.T) {
	for i := 0; i < 2; i++ { // Fresh construction cannot restore a sending capability.
		w := NewClosedWorker(Store{DB: forbiddenWorkerDB{}})
		if _, err := w.Candidates(context.Background(), ""); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, err := w.Claim(context.Background(), Hint{RunID: "already-started"}); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, err := w.Start(context.Background(), Hint{}, Lease{RunID: "already-started"}); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
}
