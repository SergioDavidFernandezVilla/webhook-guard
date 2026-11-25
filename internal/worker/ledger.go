package worker

import "github.com/sergiodavidfv/webhook-guard/internal/store"

// ledgerEntryFor builds the append-only ledger row for an applied transition.
// The event id is the idempotency key, so a retried job collides instead of
// writing twice.
func ledgerEntryFor(ev store.Event, data eventData, target store.ChargeState) store.LedgerEntry {
	return store.LedgerEntry{
		ChargeID:      ev.ChargeID,
		EntryType:     target,
		AmountMinor:   data.AmountMinor,
		Currency:      data.Currency,
		SourceEventID: ev.EventID,
	}
}
