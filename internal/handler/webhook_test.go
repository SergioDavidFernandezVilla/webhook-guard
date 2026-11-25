package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePayload_RejectsUnknownEventType(t *testing.T) {
	body := []byte(`{"event_id":"evt_1","type":"payment.charged","charge_id":"ch_1","data":{"amount_minor":100,"currency":"USD"}}`)

	_, err := decodePayload(body)
	if err == nil || !strings.Contains(err.Error(), "unknown event type") {
		t.Fatalf("decodePayload() error = %v, want unknown event type error", err)
	}
}

func TestDecodePayload_AcceptsKnownEventTypes(t *testing.T) {
	for _, eventType := range []string{
		"payment.created", "payment.authorized", "payment.captured",
		"payment.failed", "payment.refunded",
	} {
		body, err := json.Marshal(map[string]any{
			"event_id":  "evt_1",
			"type":      eventType,
			"charge_id": "ch_1",
			"data":      map[string]any{"amount_minor": 100, "currency": "USD"},
		})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}

		if _, err := decodePayload(body); err != nil {
			t.Errorf("decodePayload() for %s error = %v, want nil", eventType, err)
		}
	}
}
