package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

type paramError struct {
	name   string
	reason string
}

func (e *paramError) Error() string {
	return fmt.Sprintf("invalid %q: %s", e.name, e.reason)
}

func queryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &paramError{name: name, reason: "must be an integer"}
	}
	return value, nil
}

func queryLimit(r *http.Request) (int, error) {
	value, err := queryInt(r, "limit", 25)
	if err != nil {
		return 0, err
	}
	if value < 1 {
		value = 1
	}
	if value > 100 {
		value = 100
	}
	return value, nil
}

func queryOffset(r *http.Request) (int, error) {
	value, err := queryInt(r, "offset", 0)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		value = 0
	}
	return value, nil
}

func queryMinutes(r *http.Request) (int, error) {
	value, err := queryInt(r, "minutes", 60)
	if err != nil {
		return 0, err
	}
	if value < 1 {
		value = 1
	}
	if value > 1440 {
		value = 1440
	}
	return value, nil
}

func queryBeforeID(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("before_id")
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, &paramError{name: "before_id", reason: "must be a non-negative integer"}
	}
	return value, nil
}

func queryState(r *http.Request) (store.ChargeState, error) {
	raw := r.URL.Query().Get("state")
	switch store.ChargeState(raw) {
	case "":
		return "", nil
	case store.StateCreated, store.StateAuthorized, store.StateCaptured, store.StateFailed, store.StateRefunded:
		return store.ChargeState(raw), nil
	default:
		return "", &paramError{name: "state", reason: "must be created, authorized, captured, failed or refunded"}
	}
}

func queryEventStatus(r *http.Request) (string, error) {
	switch raw := r.URL.Query().Get("status"); raw {
	case "", "applied", "pending":
		return raw, nil
	default:
		return "", &paramError{name: "status", reason: "must be applied or pending"}
	}
}

func queryBool(r *http.Request, name string) (value bool, present bool, err error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return false, false, nil
	}
	parsed, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		return false, false, &paramError{name: name, reason: "must be true or false"}
	}
	return parsed, true, nil
}
