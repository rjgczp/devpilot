//go:build privateworker && linux

package scheduler

import "context"

// NewPrivateWorker is still closed unless the separately approved PG predicate
// attests this actual LOGIN/incarnation as private-isolated on every admission.
// The new SQL also binds final gates to incarnation mode, preventing fixture use.
func NewPrivateWorker(store Store) *Worker { return &Worker{store: store, privateMode: true} }

// PrivateReady reports only this actual private incarnation's admission.
func (w *Worker) PrivateReady(ctx context.Context) bool {
	return w != nil && w.privateMode && w.allowed(ctx)
}
