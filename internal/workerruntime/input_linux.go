//go:build privateworker && linux

package workerruntime

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

// Narrow remote transaction adapter: only the exact approved consume_input
// operation can be staged; only Commit requests its real Supervisor transaction.
// It accepts no generic SQL, DB secret, administrative identity or release flag.
type inputDB struct {
	scheduler.Database
	sup  Supervisor
	line handoff.Lineage
}
type inputTx struct {
	pgx.Tx
	db             inputDB
	staged, closed bool
	ctx            context.Context
}

func (d inputDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	if ctx.Err() != nil || o.IsoLevel != pgx.ReadCommitted {
		return nil, ErrBoundary
	}
	return &inputTx{db: d, ctx: ctx}, nil
}
func (x *inputTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	line := x.db.line
	expected, _ := json.Marshal([]any{line.SupervisorIncarnation, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, line.NamespaceIncarnation, line.FencingToken})
	actual, _ := json.Marshal(args)
	if ctx.Err() != nil || x.closed || x.staged || sql != `SELECT scheduler_private.consume_input($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bigint)` || string(expected) != string(actual) {
		return pgconn.CommandTag{}, ErrBoundary
	}
	x.staged = true
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (x *inputTx) Commit(ctx context.Context) error {
	if ctx.Err() != nil || x.closed || !x.staged {
		return ErrBoundary
	}
	x.closed = true
	r, e := x.db.sup.Call(ctx, Call{Op: "consume", Line: &x.db.line})
	if e != nil || r.Event != "input_committed" || ctx.Err() != nil {
		return scheduler.ErrCommitUnknown
	}
	return nil
}
func (x *inputTx) Rollback(context.Context) error { x.closed = true; return nil }
