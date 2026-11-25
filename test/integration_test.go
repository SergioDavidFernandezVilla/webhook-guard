//go:build integration && !naive

package integration

import (
	"net/http"
	"testing"
	"time"
)

func TestScenario61_DuplicateDeliveryIsIdempotent(t *testing.T) {
	h, s, rec := newHarness(t)
	eventID := unique("evt_61")
	chargeID := unique("ch_61")
	cleanup(t, s, []string{eventID}, []string{chargeID})

	body := webhookBody(eventID, "payment.authorized", chargeID)
	first := send(t, h, testSecret, time.Now().Unix(), body)
	if first.Code != http.StatusOK || responseStatus(t, first) != "accepted" {
		t.Fatalf("first delivery status = %d (%s), want 200 accepted", first.Code, responseStatus(t, first))
	}
	second := send(t, h, testSecret, time.Now().Unix(), body)
	if second.Code != http.StatusOK || responseStatus(t, second) != "duplicate" {
		t.Fatalf("second delivery status = %d (%s), want 200 duplicate", second.Code, responseStatus(t, second))
	}

	ids := rec.eventIDs()
	if len(ids) != 1 {
		t.Fatalf("enqueued jobs = %d, want 1", len(ids))
	}
	processEvents(t, s, ids)

	if got := eventCount(t, s, eventID); got != 1 {
		t.Errorf("webhook_events = %d, want 1", got)
	}
	if got := ledgerCount(t, s, chargeID); got != 1 {
		t.Errorf("ledger_entries = %d, want 1", got)
	}
	if got := chargeState(t, s, chargeID); got != "authorized" {
		t.Errorf("state = %q, want authorized", got)
	}
}

func TestScenario62_OutOfOrderEventsDoNotMoveStateBackward(t *testing.T) {
	h, s, rec := newHarness(t)
	chargeID := unique("ch_62")
	authorizedID := unique("evt_62_auth")
	capturedID := unique("evt_62_cap")
	staleID := unique("evt_62_stale")
	cleanup(t, s, []string{authorizedID, capturedID, staleID}, []string{chargeID})

	base := time.Now().Add(-time.Minute).Unix()
	send(t, h, testSecret, base, webhookBody(authorizedID, "payment.authorized", chargeID))
	send(t, h, testSecret, base+10, webhookBody(capturedID, "payment.captured", chargeID))
	send(t, h, testSecret, base+5, webhookBody(staleID, "payment.authorized", chargeID))

	processEvents(t, s, rec.eventIDs())

	if got := chargeState(t, s, chargeID); got != "captured" {
		t.Errorf("state = %q, want captured (must not move backward)", got)
	}
	if got := ledgerCount(t, s, chargeID); got != 2 {
		t.Errorf("ledger_entries = %d, want 2", got)
	}
	count, reason, resolved := lateEvents(t, s, chargeID)
	if count != 1 || reason != "out_of_order" || resolved {
		t.Errorf("late events = count:%d reason:%q resolved:%v, want 1/out_of_order/false", count, reason, resolved)
	}
}

func TestScenario63_InvalidSignatureAndExpiredTimestampAreRejected(t *testing.T) {
	h, s, rec := newHarness(t)
	eventID := unique("evt_63")
	chargeID := unique("ch_63")
	cleanup(t, s, []string{eventID}, []string{chargeID})

	body := webhookBody(eventID, "payment.authorized", chargeID)
	now := time.Now().Unix()

	if got := send(t, h, []byte("whsec_wrong"), now, body); got.Code != http.StatusUnauthorized {
		t.Errorf("invalid signature status = %d, want 401", got.Code)
	}
	if got := send(t, h, testSecret, now-600, body); got.Code != http.StatusUnauthorized {
		t.Errorf("expired timestamp status = %d, want 401", got.Code)
	}

	if got := eventCount(t, s, eventID); got != 0 {
		t.Errorf("webhook_events = %d, want 0", got)
	}
	if ids := rec.eventIDs(); len(ids) != 0 {
		t.Errorf("enqueued jobs = %d, want 0", len(ids))
	}
}

func TestScenario64_LateEventGoesToLateEvents(t *testing.T) {
	h, s, rec := newHarness(t)
	chargeID := unique("ch_64")
	authorizedID := unique("evt_64_auth")
	capturedID := unique("evt_64_cap")
	refundedID := unique("evt_64_refund")
	lateID := unique("evt_64_late")
	cleanup(t, s, []string{authorizedID, capturedID, refundedID, lateID}, []string{chargeID})

	base := time.Now().Add(-time.Minute).Unix()
	send(t, h, testSecret, base, webhookBody(authorizedID, "payment.authorized", chargeID))
	send(t, h, testSecret, base+10, webhookBody(capturedID, "payment.captured", chargeID))
	send(t, h, testSecret, base+20, webhookBody(refundedID, "payment.refunded", chargeID))
	send(t, h, testSecret, base+30, webhookBody(lateID, "payment.captured", chargeID))

	processEvents(t, s, rec.eventIDs())

	if got := chargeState(t, s, chargeID); got != "refunded" {
		t.Errorf("state = %q, want refunded", got)
	}
	if got := ledgerCount(t, s, chargeID); got != 3 {
		t.Errorf("ledger_entries = %d, want 3", got)
	}
	count, reason, resolved := lateEvents(t, s, chargeID)
	if count != 1 || reason != "charge_closed" || resolved {
		t.Errorf("late events = count:%d reason:%q resolved:%v, want 1/charge_closed/false", count, reason, resolved)
	}
}

func TestScenario65_GuardedBatchComparison(t *testing.T) {
	h, s, rec := newHarness(t)

	const batch = 5
	duplicates := 0
	serverErrors := 0
	eventIDs := make([]string, 0, batch)
	chargeIDs := make([]string, 0, batch)
	for i := 0; i < batch; i++ {
		eventID := unique("evt_65_dup")
		chargeID := unique("ch_65_dup")
		eventIDs = append(eventIDs, eventID)
		chargeIDs = append(chargeIDs, chargeID)

		body := webhookBody(eventID, "payment.authorized", chargeID)
		send(t, h, testSecret, time.Now().Unix(), body)
		second := send(t, h, testSecret, time.Now().Unix(), body)
		if responseStatus(t, second) == "duplicate" {
			duplicates++
		}
		if second.Code >= http.StatusInternalServerError {
			serverErrors++
		}
	}
	cleanup(t, s, eventIDs, chargeIDs)
	processEvents(t, s, rec.eventIDs())
	processed := len(rec.eventIDs())

	invalidBefore := metricTotal(t, s, "jobs_rejected_transition")

	chargeID := unique("ch_65_ooo")
	capturedID := unique("evt_65_cap")
	authorizedID := unique("evt_65_auth")
	cleanup(t, s, []string{capturedID, authorizedID}, []string{chargeID})

	base := time.Now().Add(-time.Minute).Unix()
	send(t, h, testSecret, base, webhookBody(capturedID, "payment.captured", chargeID))
	send(t, h, testSecret, base+10, webhookBody(authorizedID, "payment.authorized", chargeID))
	processEvents(t, s, rec.eventIDs()[processed:])

	invalidAfter := metricTotal(t, s, "jobs_rejected_transition")
	invalidTransitions := invalidAfter - invalidBefore

	t.Logf("guarded batch: events=%d duplicates=%d server_errors=%d invalid_transitions_rejected=%d",
		batch, duplicates, serverErrors, invalidTransitions)

	if duplicates != batch {
		t.Errorf("duplicates = %d, want %d", duplicates, batch)
	}
	if serverErrors != 0 {
		t.Errorf("server errors = %d, want 0", serverErrors)
	}
	if invalidTransitions != 1 {
		t.Errorf("invalid transitions rejected = %d, want 1", invalidTransitions)
	}
	if got := chargeState(t, s, chargeID); got != "authorized" {
		t.Errorf("out-of-order charge state = %q, want authorized", got)
	}
	if got := ledgerCount(t, s, chargeID); got != 1 {
		t.Errorf("out-of-order ledger entries = %d, want 1 (invalid captured rejected)", got)
	}
}
