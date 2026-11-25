// Package api exposes the dashboard's read-only JSON API. It never mutates
// charges or the ledger, never returns the signing secret and never talks to a
// client about SQL details.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/reconcile"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

const (
	defaultWindow = 60 * time.Minute
	pingTimeout   = 500 * time.Millisecond
)

// Options configures the dashboard API.
type Options struct {
	// DemoMode registers the demo write endpoint. It is off by default.
	DemoMode bool
	// DemoSecret is the HMAC key used to sign demo webhooks. It never leaves
	// the process.
	DemoSecret []byte
	// Webhook is the real ingestion handler invoked by the demo endpoint, so
	// signature verification and TX1 are exercised instead of bypassed.
	Webhook http.Handler
	// LoadgenIngest registers the gated loadgen result endpoint. It is off by
	// default so the dashboard stays read-only.
	LoadgenIngest bool
	// Logger receives request and demo diagnostics. Defaults to slog.Default.
	Logger *slog.Logger
}

// Handler serves the dashboard JSON API.
type Handler struct {
	store   *store.Store
	logger  *slog.Logger
	now     func() time.Time
	demo    *demoEndpoint
	loadgen *loadgenIngest
}

// New builds the dashboard API. The demo endpoint is registered only when
// DemoMode is on, a webhook handler is provided and the secret is not empty.
func New(st *store.Store, opts Options) *Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{store: st, logger: logger, now: time.Now}
	if opts.DemoMode && opts.Webhook != nil && len(opts.DemoSecret) > 0 {
		h.demo = newDemoEndpoint(opts.Webhook, opts.DemoSecret, logger, h.now)
	}
	if opts.LoadgenIngest {
		h.loadgen = newLoadgenIngest(h.store.InsertLoadgenRun, logger)
	}
	return h
}

// Register mounts the API routes on the given router. The caller decides the
// prefix (the service uses /api/v1).
func (h *Handler) Register(r chi.Router) {
	r.Get("/capabilities", h.capabilities)
	r.Get("/overview", h.overview)
	r.Get("/metrics", h.metrics)
	r.Get("/events", h.listEvents)
	r.Get("/events/{eventID}", h.getEvent)
	r.Get("/charges", h.listCharges)
	r.Get("/charges/{chargeID}", h.getCharge)
	r.Get("/late-events", h.listLateEvents)
	r.Get("/reconciliation/latest", h.latestReport)
	r.Get("/reconciliation/reports", h.listReports)
	r.Get("/loadgen/runs", h.listLoadgenRuns)
	r.Get("/loadgen/latest", h.latestLoadgenRuns)
	if h.demo != nil {
		r.Post("/demo/webhooks", h.demo.serveHTTP)
	}
	if h.loadgen != nil {
		r.Post("/loadgen/runs", h.loadgen.serveHTTP)
	}
}

// NotFound writes a JSON 404 for unmatched API routes.
func (h *Handler) NotFound(w http.ResponseWriter, _ *http.Request) {
	h.fail(w, http.StatusNotFound, "not_found", "route not found")
}

// MethodNotAllowed writes a JSON 405 for a known route with the wrong method.
func (h *Handler) MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	h.fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func (h *Handler) capabilities(w http.ResponseWriter, _ *http.Request) {
	h.respond(w, http.StatusOK, capabilitiesDTO{DemoEnabled: h.demo != nil})
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pingStart := h.now()
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	pingErr := h.store.Ping(pingCtx)
	cancel()
	database := overviewDatabase{
		Ready:     pingErr == nil,
		LatencyMS: float64(h.now().Sub(pingStart).Microseconds()) / 1000,
	}

	events, err := h.store.CountEvents(ctx)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	byState, err := h.store.CountChargesByState(ctx)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	ledgerEntries, err := h.store.Count(ctx)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	latePending, lateResolved, err := h.store.CountLateEventsByResolution(ctx)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	points, err := h.store.ListMetrics(ctx, h.now().Add(-defaultWindow))
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	states := map[string]int64{
		string(store.StateCreated):    0,
		string(store.StateAuthorized): 0,
		string(store.StateCaptured):   0,
		string(store.StateFailed):     0,
		string(store.StateRefunded):   0,
	}
	var chargeTotal int64
	for state, count := range byState {
		states[string(state)] = count
		chargeTotal += count
	}

	totals := make(map[string]int64)
	for _, p := range points {
		totals[p.Name] += p.Count
	}

	overview := overviewDTO{
		GeneratedAt:   h.now().UTC(),
		Database:      database,
		Events:        overviewEvents{Total: events.Total, Applied: events.Applied, Pending: events.Pending},
		Charges:       overviewCharges{Total: chargeTotal, ByState: states},
		LedgerEntries: ledgerEntries,
		LateEvents:    overviewLateEvents{Pending: latePending, Resolved: lateResolved},
		MetricTotals:  totals,
	}

	batchID, raw, createdAt, err := h.store.LatestReport(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No report yet: the UI renders an empty state.
	case err != nil:
		h.failInternal(w, r, err)
		return
	default:
		var report reconcile.Report
		if err := json.Unmarshal(raw, &report); err != nil {
			h.failInternal(w, r, fmt.Errorf("decode latest report: %w", err))
			return
		}
		overview.LatestReport = &overviewReport{
			BatchID:   batchID,
			CreatedAt: createdAt,
			Checked:   report.Checked,
			Counts:    report.Counts,
			HasDiffs:  report.HasDiffs(),
		}
	}

	h.respond(w, http.StatusOK, overview)
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	minutes, err := queryMinutes(r)
	if err != nil {
		h.failParam(w, err)
		return
	}

	points, err := h.store.ListMetrics(r.Context(), h.now().Add(-time.Duration(minutes)*time.Minute))
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	items := make([]metricPointDTO, 0, len(points))
	for _, p := range points {
		items = append(items, metricPointDTO{
			Name:          p.Name,
			WindowStart:   p.WindowStart,
			WindowSeconds: p.WindowSeconds,
			Count:         p.Count,
		})
	}
	h.respond(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) listCharges(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	offset, err := queryOffset(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	state, err := queryState(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	filter := store.ChargeFilter{
		Limit:  limit,
		Offset: offset,
		State:  state,
		Query:  r.URL.Query().Get("q"),
	}

	total, err := h.store.CountCharges(r.Context(), filter)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	charges, err := h.store.ListCharges(r.Context(), filter)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	items := make([]chargeDTO, 0, len(charges))
	for _, c := range charges {
		items = append(items, newChargeDTO(c))
	}
	h.respond(w, http.StatusOK, listChargesDTO{Items: items, Total: total, Limit: limit, Offset: offset})
}

func (h *Handler) getCharge(w http.ResponseWriter, r *http.Request) {
	chargeID := chi.URLParam(r, "chargeID")
	ctx := r.Context()

	charge, err := h.store.GetCharge(ctx, chargeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.fail(w, http.StatusNotFound, "not_found", "charge not found")
			return
		}
		h.failInternal(w, r, err)
		return
	}

	entries, err := h.store.ListByCharge(ctx, chargeID)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	total, err := h.store.SumByCharge(ctx, chargeID)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	events, err := h.store.ListEvents(ctx, store.EventFilter{ChargeID: chargeID, Limit: 50})
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	late, err := h.store.ListLateEvents(ctx, store.LateEventFilter{ChargeID: chargeID, Limit: 50})
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	detail := chargeDetailDTO{
		Charge:      newChargeDTO(charge),
		Ledger:      make([]ledgerEntryDTO, 0, len(entries)),
		LedgerTotal: total,
		Events:      make([]eventDTO, 0, len(events)),
		LateEvents:  make([]lateEventDTO, 0, len(late)),
	}
	for _, e := range entries {
		detail.Ledger = append(detail.Ledger, newLedgerEntryDTO(e))
	}
	for _, e := range events {
		detail.Events = append(detail.Events, newEventDTO(e))
	}
	for _, e := range late {
		detail.LateEvents = append(detail.LateEvents, newLateEventDTO(e))
	}
	h.respond(w, http.StatusOK, detail)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	beforeID, err := queryBeforeID(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	status, err := queryEventStatus(r)
	if err != nil {
		h.failParam(w, err)
		return
	}

	events, err := h.store.ListEvents(r.Context(), store.EventFilter{
		Limit:    limit,
		BeforeID: beforeID,
		Status:   status,
		Type:     r.URL.Query().Get("type"),
		ChargeID: r.URL.Query().Get("charge_id"),
	})
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	resp := listEventsDTO{Items: make([]eventDTO, 0, len(events))}
	for _, ev := range events {
		resp.Items = append(resp.Items, newEventDTO(ev))
	}
	if len(events) == limit && events[len(events)-1].ID > 0 {
		next := events[len(events)-1].ID
		resp.NextBeforeID = &next
	}
	h.respond(w, http.StatusOK, resp)
}

func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "eventID")
	ctx := r.Context()

	ev, err := h.store.GetEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.fail(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		h.failInternal(w, r, err)
		return
	}

	resp := eventDetailResponse{Event: newEventDetailDTO(ev)}
	entry, err := h.store.GetBySourceEvent(ctx, eventID)
	switch {
	case err == nil:
		dto := newLedgerEntryDTO(entry)
		resp.Ledger = &dto
	case errors.Is(err, store.ErrNotFound):
	default:
		h.failInternal(w, r, err)
		return
	}

	late, err := h.store.GetByEventID(ctx, eventID)
	switch {
	case err == nil:
		dto := newLateEventDTO(late)
		resp.Late = &dto
	case errors.Is(err, store.ErrNotFound):
	default:
		h.failInternal(w, r, err)
		return
	}

	h.respond(w, http.StatusOK, resp)
}

func (h *Handler) listLateEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	offset, err := queryOffset(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	resolved, hasResolved, err := queryBool(r, "resolved")
	if err != nil {
		h.failParam(w, err)
		return
	}
	filter := store.LateEventFilter{
		Limit:    limit,
		Offset:   offset,
		ChargeID: r.URL.Query().Get("charge_id"),
	}
	if hasResolved {
		filter.Resolved = &resolved
	}

	total, err := h.store.CountLateEvents(r.Context(), filter)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	events, err := h.store.ListLateEvents(r.Context(), filter)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	items := make([]lateEventDTO, 0, len(events))
	for _, ev := range events {
		items = append(items, newLateEventDTO(ev))
	}
	h.respond(w, http.StatusOK, listLateEventsDTO{Items: items, Total: total, Limit: limit, Offset: offset})
}

func (h *Handler) latestReport(w http.ResponseWriter, r *http.Request) {
	batchID, raw, createdAt, err := h.store.LatestReport(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.respond(w, http.StatusOK, latestReportResponse{Report: nil})
			return
		}
		h.failInternal(w, r, err)
		return
	}

	var report reconcile.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		h.failInternal(w, r, fmt.Errorf("decode latest report: %w", err))
		return
	}
	h.respond(w, http.StatusOK, latestReportResponse{Report: &latestReportDTO{
		BatchID:   batchID,
		CreatedAt: createdAt,
		Report:    report,
	}})
}

func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	stored, err := h.store.ListReports(r.Context(), limit)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}

	items := make([]storedReportDTO, 0, len(stored))
	for _, s := range stored {
		var report reconcile.Report
		if err := json.Unmarshal(s.Report, &report); err != nil {
			h.failInternal(w, r, fmt.Errorf("decode stored report %d: %w", s.ID, err))
			return
		}
		items = append(items, storedReportDTO{
			ID:        s.ID,
			BatchID:   s.BatchID,
			CreatedAt: s.CreatedAt,
			Report:    report,
		})
	}
	h.respond(w, http.StatusOK, listReportsDTO{Items: items})
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *Handler) respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("write api response", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, status int, code, message string) {
	h.respond(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func (h *Handler) failParam(w http.ResponseWriter, err error) {
	h.fail(w, http.StatusBadRequest, "invalid_parameter", err.Error())
}

func (h *Handler) failInternal(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "dashboard api failed",
		slog.String("path", r.URL.Path),
		slog.Any("error", err),
	)
	h.fail(w, http.StatusInternalServerError, "internal_error", "unexpected error")
}
