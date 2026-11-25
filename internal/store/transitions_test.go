package store

import "testing"

func TestCanTransition_ForwardAllowedBackwardRejected(t *testing.T) {
	cases := []struct {
		name string
		from ChargeState
		to   ChargeState
		want bool
	}{
		{name: "created to authorized", from: StateCreated, to: StateAuthorized, want: true},
		{name: "authorized to captured", from: StateAuthorized, to: StateCaptured, want: true},
		{name: "authorized to failed", from: StateAuthorized, to: StateFailed, want: true},
		{name: "captured to refunded", from: StateCaptured, to: StateRefunded, want: true},
		{name: "created to captured skips step", from: StateCreated, to: StateCaptured, want: false},
		{name: "captured to authorized is backward", from: StateCaptured, to: StateAuthorized, want: false},
		{name: "refunded to created is backward", from: StateRefunded, to: StateCreated, want: false},
		{name: "failed to authorized is backward", from: StateFailed, to: StateAuthorized, want: false},
		{name: "same state is not a transition", from: StateAuthorized, to: StateAuthorized, want: false},
		{name: "unknown source", from: ChargeState("bogus"), to: StateCreated, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanTransition(tc.from, tc.to); got != tc.want {
				t.Errorf("CanTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}
