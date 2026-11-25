package api

import (
	"encoding/json"
	"time"

	"github.com/sergiodavidfv/webhook-guard/internal/reconcile"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

type chargeDTO struct {
	ChargeID    string    `json:"charge_id"`
	State       string    `json:"state"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastEventID string    `json:"last_event_id"`
}

func newChargeDTO(c store.Charge) chargeDTO {
	return chargeDTO{
		ChargeID:    c.ChargeID,
		State:       string(c.State),
		AmountMinor: c.AmountMinor,
		Currency:    c.Currency,
		UpdatedAt:   c.UpdatedAt,
		LastEventID: c.LastEventID,
	}
}

type eventDTO struct {
	ID                 int64      `json:"id"`
	EventID            string     `json:"event_id"`
	Type               string     `json:"type"`
	ChargeID           string     `json:"charge_id"`
	SignatureTimestamp int64      `json:"signature_timestamp"`
	CorrelationID      string     `json:"correlation_id"`
	ReceivedAt         time.Time  `json:"received_at"`
	AppliedAt          *time.Time `json:"applied_at"`
}

func newEventDTO(ev store.Event) eventDTO {
	return eventDTO{
		ID:                 ev.ID,
		EventID:            ev.EventID,
		Type:               ev.Type,
		ChargeID:           ev.ChargeID,
		SignatureTimestamp: ev.SignatureTimestamp,
		CorrelationID:      ev.CorrelationID,
		ReceivedAt:         ev.ReceivedAt,
		AppliedAt:          ev.AppliedAt,
	}
}

type eventDetailDTO struct {
	eventDTO
	Payload json.RawMessage `json:"payload"`
}

func newEventDetailDTO(ev store.Event) eventDetailDTO {
	return eventDetailDTO{eventDTO: newEventDTO(ev), Payload: ev.Payload}
}

type ledgerEntryDTO struct {
	ID            int64     `json:"id"`
	ChargeID      string    `json:"charge_id"`
	EntryType     string    `json:"entry_type"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	SourceEventID string    `json:"source_event_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func newLedgerEntryDTO(e store.LedgerEntry) ledgerEntryDTO {
	return ledgerEntryDTO{
		ID:            e.ID,
		ChargeID:      e.ChargeID,
		EntryType:     string(e.EntryType),
		AmountMinor:   e.AmountMinor,
		Currency:      e.Currency,
		SourceEventID: e.SourceEventID,
		CreatedAt:     e.CreatedAt,
	}
}

type lateEventDTO struct {
	ID         int64      `json:"id"`
	EventID    string     `json:"event_id"`
	ChargeID   string     `json:"charge_id"`
	Reason     string     `json:"reason"`
	ReceivedAt time.Time  `json:"received_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
}

func newLateEventDTO(ev store.LateEvent) lateEventDTO {
	return lateEventDTO{
		ID:         ev.ID,
		EventID:    ev.EventID,
		ChargeID:   ev.ChargeID,
		Reason:     ev.Reason,
		ReceivedAt: ev.ReceivedAt,
		ResolvedAt: ev.ResolvedAt,
	}
}

type metricPointDTO struct {
	Name          string    `json:"name"`
	WindowStart   time.Time `json:"window_start"`
	WindowSeconds int       `json:"window_seconds"`
	Count         int64     `json:"count"`
}

type overviewDatabase struct {
	Ready     bool    `json:"ready"`
	LatencyMS float64 `json:"latency_ms"`
}

type overviewEvents struct {
	Total   int64 `json:"total"`
	Applied int64 `json:"applied"`
	Pending int64 `json:"pending"`
}

type overviewCharges struct {
	Total   int64            `json:"total"`
	ByState map[string]int64 `json:"by_state"`
}

type overviewLateEvents struct {
	Pending  int64 `json:"pending"`
	Resolved int64 `json:"resolved"`
}

type overviewReport struct {
	BatchID   string         `json:"batch_id"`
	CreatedAt time.Time      `json:"created_at"`
	Checked   int            `json:"checked"`
	Counts    map[string]int `json:"counts"`
	HasDiffs  bool           `json:"has_diffs"`
}

type overviewDTO struct {
	GeneratedAt   time.Time          `json:"generated_at"`
	Database      overviewDatabase   `json:"database"`
	Events        overviewEvents     `json:"events"`
	Charges       overviewCharges    `json:"charges"`
	LedgerEntries int64              `json:"ledger_entries"`
	LateEvents    overviewLateEvents `json:"late_events"`
	MetricTotals  map[string]int64   `json:"metric_totals"`
	LatestReport  *overviewReport    `json:"latest_report"`
}

type listChargesDTO struct {
	Items  []chargeDTO `json:"items"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

type chargeDetailDTO struct {
	Charge      chargeDTO        `json:"charge"`
	Ledger      []ledgerEntryDTO `json:"ledger"`
	LedgerTotal int64            `json:"ledger_total"`
	Events      []eventDTO       `json:"events"`
	LateEvents  []lateEventDTO   `json:"late_events"`
}

type listEventsDTO struct {
	Items        []eventDTO `json:"items"`
	NextBeforeID *int64     `json:"next_before_id"`
}

type eventDetailResponse struct {
	Event  eventDetailDTO  `json:"event"`
	Ledger *ledgerEntryDTO `json:"ledger"`
	Late   *lateEventDTO   `json:"late"`
}

type listLateEventsDTO struct {
	Items  []lateEventDTO `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type latestReportDTO struct {
	BatchID   string           `json:"batch_id"`
	CreatedAt time.Time        `json:"created_at"`
	Report    reconcile.Report `json:"report"`
}

type latestReportResponse struct {
	Report *latestReportDTO `json:"report"`
}

type storedReportDTO struct {
	ID        int64            `json:"id"`
	BatchID   string           `json:"batch_id"`
	CreatedAt time.Time        `json:"created_at"`
	Report    reconcile.Report `json:"report"`
}

type listReportsDTO struct {
	Items []storedReportDTO `json:"items"`
}

type capabilitiesDTO struct {
	DemoEnabled bool `json:"demo_enabled"`
}
