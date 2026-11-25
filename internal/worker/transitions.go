package worker

import "github.com/sergiodavidfv/webhook-guard/internal/store"

// ChargeState is the persisted charge lifecycle. The canonical state set and
// transition table live in store so guard and worker share one source of truth
// without an import cycle.
type ChargeState = store.ChargeState

const (
	StateCreated    = store.StateCreated
	StateAuthorized = store.StateAuthorized
	StateCaptured   = store.StateCaptured
	StateFailed     = store.StateFailed
	StateRefunded   = store.StateRefunded
)

// ErrInvalidTransition is re-exported from store.
var ErrInvalidTransition = store.ErrInvalidTransition

// CanTransition reports whether from -> to is allowed.
func CanTransition(from, to ChargeState) bool {
	return store.CanTransition(from, to)
}
