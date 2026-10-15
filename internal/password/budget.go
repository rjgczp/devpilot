package password

// Budget is injected once by the composition root into both endpoint handlers.
// Acquire never queues. Cancellation cannot release still-running Argon work.
type Budget struct{ slots chan struct{} }

func NewBudget() *Budget { return &Budget{slots: make(chan struct{}, 2)} }

func (b *Budget) Acquire() bool {
	select {
	case b.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (b *Budget) Release()    { <-b.slots }
func (b *Budget) Active() int { return len(b.slots) }
