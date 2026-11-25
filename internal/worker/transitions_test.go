package worker

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestTransitions_NoBackwardTransitions(t *testing.T) {
	order := []ChargeState{StateCreated, StateAuthorized, StateCaptured, StateFailed, StateRefunded}
	for i := range order {
		for j := 0; j <= i; j++ {
			if CanTransition(order[i], order[j]) {
				t.Errorf("CanTransition(%q, %q) = true, want false (backward or self)", order[i], order[j])
			}
		}
	}

	forward := []struct {
		from ChargeState
		to   ChargeState
	}{
		{StateCreated, StateAuthorized},
		{StateAuthorized, StateCaptured},
		{StateAuthorized, StateFailed},
		{StateCaptured, StateRefunded},
	}
	for _, transition := range forward {
		if !CanTransition(transition.from, transition.to) {
			t.Errorf("CanTransition(%q, %q) = false, want true", transition.from, transition.to)
		}
	}
}

func TestTransitions_EnumMatchesSQLCheck(t *testing.T) {
	path := filepath.Join("..", "..", "migrations", "002_charges.up.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	check := regexp.MustCompile(`state IN \(([^)]*)\)`).FindSubmatch(content)
	if check == nil {
		t.Fatalf("could not find state IN (...) in %s", path)
	}

	sqlStates := map[string]bool{}
	for _, match := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(string(check[1]), -1) {
		sqlStates[match[1]] = true
	}

	workerStates := []ChargeState{StateCreated, StateAuthorized, StateCaptured, StateFailed, StateRefunded}
	if len(sqlStates) != len(workerStates) {
		t.Fatalf("SQL has %d states, worker has %d", len(sqlStates), len(workerStates))
	}
	for _, state := range workerStates {
		if !sqlStates[string(state)] {
			t.Errorf("SQL CHECK is missing worker state %q", state)
		}
	}
}
