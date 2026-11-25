package store

import "errors"

// ErrInvalidTransition reports a charge state change that the domain forbids.
var ErrInvalidTransition = errors.New("store: invalid charge transition")

// allowedTransitions is the single source of truth for charge ordering. A state
// absent as a key has no outgoing transitions. The worker package delegates to
// CanTransition so guard and worker cannot drift apart.
var allowedTransitions = map[ChargeState][]ChargeState{
	StateCreated:    {StateAuthorized},
	StateAuthorized: {StateCaptured, StateFailed},
	StateCaptured:   {StateRefunded},
	StateFailed:     {},
	StateRefunded:   {},
}

// CanTransition reports whether from -> to is allowed. Backward and unknown
// transitions are rejected.
func CanTransition(from, to ChargeState) bool {
	for _, candidate := range allowedTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}
