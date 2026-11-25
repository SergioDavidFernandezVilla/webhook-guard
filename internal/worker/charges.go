package worker

import (
	"encoding/json"
	"fmt"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

// stateForEventType maps the gateway event type to the charge state it applies.
// The mapping is intentionally explicit: an unknown type is never applied.
func stateForEventType(eventType string) (store.ChargeState, bool) {
	switch eventType {
	case store.EventTypeCreated:
		return store.StateCreated, true
	case "payment.authorized":
		return store.StateAuthorized, true
	case "payment.captured":
		return store.StateCaptured, true
	case "payment.failed":
		return store.StateFailed, true
	case "payment.refunded":
		return store.StateRefunded, true
	default:
		return "", false
	}
}

// IsKnownEventType reports whether the event type is one the domain accepts.
// The handler rejects anything else before persisting; the worker keeps the
// same check as defense in depth.
func IsKnownEventType(eventType string) bool {
	_, ok := stateForEventType(eventType)
	return ok
}

// eventData is the money movement carried inside the webhook payload's "data".
type eventData struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type eventEnvelope struct {
	Data eventData `json:"data"`
}

func parseEventData(payload json.RawMessage) (eventData, error) {
	var envelope eventEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return eventData{}, fmt.Errorf("decode event payload: %w", err)
	}

	data := envelope.Data
	if data.AmountMinor < 0 {
		return eventData{}, fmt.Errorf("amount_minor must be non-negative, got %d", data.AmountMinor)
	}
	if len(data.Currency) != 3 {
		return eventData{}, fmt.Errorf("currency must be three letters, got %q", data.Currency)
	}
	for _, r := range data.Currency {
		if r < 'A' || r > 'Z' {
			return eventData{}, fmt.Errorf("currency must be uppercase letters, got %q", data.Currency)
		}
	}
	return data, nil
}
