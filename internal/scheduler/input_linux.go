//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
)

// InputController is the already-approved independent Supervisor DB identity.
// It owns consume_input EXECUTE; it is never handed to the Probe process.
type InputController struct {
	DB          Database
	Incarnation string
}
type InputPipes struct{ Envelope, Receipt *os.File }

func (p InputPipes) Close() {
	if p.Envelope != nil {
		_ = p.Envelope.Close()
	}
	if p.Receipt != nil {
		_ = p.Receipt.Close()
	}
}

// IssueInput may only run after confirmed Start. It commits the independent,
// once-only input consumption before making any bytes available to the child.
// No DB connection/transaction is retained while creating/writing IPC.
func (s StartSnapshot) IssueInput(ctx context.Context, c InputController) (InputPipes, error) {
	frame, receipt, err := s.takeFrame()
	if err != nil {
		return InputPipes{}, err
	}
	if c.DB == nil {
		return InputPipes{}, ErrUnavailable
	}
	if !receipt.Lineage.Valid() || !receipt.PermitDeadline.After(time.Now()) {
		return InputPipes{}, ErrUnavailable
	}
	ack, err := handoff.ReceiptFrame(receipt)
	if err != nil {
		return InputPipes{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, transactionBudget)
	defer cancel()
	tx, err := c.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return InputPipes{}, ErrUnavailable
	}
	defer rollback(tx)
	v := receipt.Lineage
	_, err = tx.Exec(ctx, `SELECT scheduler_private.consume_input($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bigint)`, c.Incarnation, v.WorkspaceID, v.ProjectID, v.MonitorID, v.RunID, v.NamespaceIncarnation, v.FencingToken)
	if err != nil {
		return InputPipes{}, databaseError(err)
	}
	if tx.Commit(ctx) != nil {
		return InputPipes{}, ErrCommitUnknown
	}
	if ctx.Err() != nil {
		return InputPipes{}, ErrUnavailable
	}
	// The confirmed commit has already returned the connection to its pool.
	var pipes InputPipes
	makePipe := func(data []byte) (*os.File, error) {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, ErrUnavailable
		}
		defer w.Close()
		deadline := time.Now().Add(2 * time.Second)
		if receipt.PermitDeadline.Before(deadline) {
			deadline = receipt.PermitDeadline
		}
		if w.SetWriteDeadline(deadline) != nil {
			r.Close()
			return nil, ErrUnavailable
		}
		n, err := w.Write(data)
		if err != nil || n != len(data) {
			r.Close()
			return nil, ErrUnavailable
		}
		return r, nil
	}
	pipes.Envelope, err = makePipe(frame)
	if err != nil {
		return InputPipes{}, err
	}
	pipes.Receipt, err = makePipe(ack)
	if err != nil {
		pipes.Close()
		return InputPipes{}, err
	}
	if ctx.Err() != nil || !receipt.PermitDeadline.After(time.Now()) {
		pipes.Close()
		return InputPipes{}, ErrUnavailable
	}
	return pipes, nil
}
