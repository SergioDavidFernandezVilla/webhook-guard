//go:build integration && naive

package integration

import (
	"net/http"
	"testing"
	"time"
)

func TestScenario65_NaiveBatchComparison(t *testing.T) {
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

	invalidBefore := metricTotal(t, s, "jobs_rejected_transition")

	chargeID := unique("ch_65_ooo")
	capturedID := unique("evt_65_cap")
	authorizedID := unique("evt_65_auth")
	cleanup(t, s, []string{capturedID, authorizedID}, []string{chargeID})

	base := time.Now().Add(-time.Minute).Unix()
	send(t, h, testSecret, base, webhookBody(capturedID, "payment.captured", chargeID))
	send(t, h, testSecret, base+10, webhookBody(authorizedID, "payment.authorized", chargeID))
	all := rec.eventIDs()
	processEvents(t, s, all[len(all)-2:])

	invalidAfter := metricTotal(t, s, "jobs_rejected_transition")
	invalidTransitions := invalidAfter - invalidBefore

	t.Logf("naive batch: events=%d duplicates=%d server_errors=%d invalid_transitions_rejected=%d",
		batch, duplicates, serverErrors, invalidTransitions)

	if duplicates != 0 {
		t.Errorf("duplicates = %d, want 0 (naive returns errors instead)", duplicates)
	}
	if serverErrors != batch {
		t.Errorf("server errors = %d, want %d", serverErrors, batch)
	}
	if invalidTransitions != 0 {
		t.Errorf("invalid transitions rejected = %d, want 0 (naive applies anything)", invalidTransitions)
	}
	if got := chargeState(t, s, chargeID); got != "authorized" {
		t.Errorf("state = %q, want authorized (backward transition applied)", got)
	}
	if got := ledgerCount(t, s, chargeID); got != 2 {
		t.Errorf("ledger entries = %d, want 2 (both transitions applied)", got)
	}
}
