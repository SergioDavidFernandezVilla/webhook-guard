// Package handler ingests signed webhook requests: it verifies the signature,
// persists the event once and enqueues the job in the same transaction (TX1).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/sergiodavidfv/webhook-guard/internal/guard"
	"github.com/sergiodavidfv/webhook-guard/internal/middleware"
	"github.com/sergiodavidfv/webhook-guard/internal/signature"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
	"github.com/sergiodavidfv/webhook-guard/internal/worker"
)

const (
	// DefaultMaxBodyBytes caps the raw request body at 1 MiB.
	DefaultMaxBodyBytes int64 = 1 << 20
	signatureHeader           = "X-Signature"

	metricWebhooksReceived          = "webhooks_received"
	metricWebhooksDuplicate         = "webhooks_duplicate"
	metricWebhooksRejectedSignature = "webhooks_rejected_signature"
	metricWindowSeconds             = 60
)

var errBodyTooLarge = errors.New("handler: body too large")

// JobInserter enqueues a job inside an existing transaction. It is satisfied by
// *river.Client[pgx.Tx] in production and by a fake in tests.
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Handler is the webhook ingestion HTTP handler.
type Handler struct {
	store        *store.Store
	guard        guard.Guard
	inserter     JobInserter
	secret       []byte
	tolerance    time.Duration
	queue        string
	maxBodyBytes int64
}

// New builds the webhook handler. secret is the HMAC key, tolerance the
// accepted timestamp window and queue the River queue jobs are enqueued to
// (it must match the queue the worker consumes).
func New(st *store.Store, g guard.Guard, inserter JobInserter, secret []byte, tolerance time.Duration, queue string) *Handler {
	return &Handler{
		store:        st,
		guard:        g,
		inserter:     inserter,
		secret:       secret,
		tolerance:    tolerance,
		queue:        queue,
		maxBodyBytes: DefaultMaxBodyBytes,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()
	logger := middleware.LoggerFrom(ctx)

	body, err := readBody(w, r, h.maxBodyBytes)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			logger.WarnContext(ctx, "webhook rejected: body too large")
			writeError(ctx, w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		logger.WarnContext(ctx, "webhook rejected: unreadable body", slog.Any("error", err))
		writeError(ctx, w, http.StatusBadRequest, "invalid body")
		return
	}

	header := r.Header.Get(signatureHeader)
	parsed, err := signature.Parse(header)
	if err != nil {
		logger.WarnContext(ctx, "webhook rejected: malformed signature header", slog.Any("error", err))
		h.recordRejectedSignature(ctx)
		writeError(ctx, w, http.StatusBadRequest, "malformed signature")
		return
	}
	if err := signature.Verify(h.secret, header, body, time.Now(), h.tolerance); err != nil {
		h.recordRejectedSignature(ctx)
		if errors.Is(err, signature.ErrTimestampOutOfTolerance) {
			logger.WarnContext(ctx, "webhook rejected: timestamp out of tolerance")
			writeError(ctx, w, http.StatusUnauthorized, "signature expired")
			return
		}
		logger.WarnContext(ctx, "webhook rejected: signature mismatch", slog.Any("error", err))
		writeError(ctx, w, http.StatusUnauthorized, "invalid signature")
		return
	}

	payload, err := decodePayload(body)
	if err != nil {
		logger.WarnContext(ctx, "webhook rejected: invalid payload", slog.Any("error", err))
		writeError(ctx, w, http.StatusBadRequest, "invalid payload")
		return
	}

	logger = logger.With(
		slog.String("event_id", payload.EventID),
		slog.String("charge_id", payload.ChargeID),
	)

	status := "accepted"
	inserted := false
	err = h.store.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		var insertErr error
		inserted, insertErr = h.guard.InsertEvent(txCtx, tx, store.Event{
			EventID:            payload.EventID,
			Type:               payload.Type,
			ChargeID:           payload.ChargeID,
			Payload:            body,
			SignatureTimestamp: parsed.Timestamp,
			CorrelationID:      chimw.GetReqID(ctx),
		})
		if insertErr != nil {
			return fmt.Errorf("insert webhook event: %w", insertErr)
		}
		if err := h.addMetric(txCtx, tx, metricWebhooksReceived, 1); err != nil {
			return err
		}
		if !inserted {
			status = "duplicate"
			if err := h.addMetric(txCtx, tx, metricWebhooksDuplicate, 1); err != nil {
				return err
			}
			return nil
		}

		if _, err := h.inserter.InsertTx(txCtx, tx, worker.WebhookArgs{EventID: payload.EventID}, &river.InsertOpts{Queue: h.queue}); err != nil {
			return fmt.Errorf("enqueue webhook job: %w", err)
		}
		return nil
	})
	if err != nil {
		logger.ErrorContext(ctx, "webhook TX1 failed", slog.Any("error", err))
		writeError(ctx, w, http.StatusInternalServerError, "internal error")
		return
	}

	logger.InfoContext(ctx, "webhook processed",
		slog.String("status", status),
		slog.Bool("inserted", inserted),
		slog.Duration("duration", time.Since(start)),
	)
	writeJSON(ctx, w, http.StatusOK, map[string]string{"status": status})
}

type payload struct {
	EventID  string          `json:"event_id"`
	Type     string          `json:"type"`
	ChargeID string          `json:"charge_id"`
	Data     json.RawMessage `json:"data"`
}

func decodePayload(body []byte) (payload, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return payload{}, fmt.Errorf("decode json: %w", err)
	}
	switch {
	case p.EventID == "":
		return payload{}, errors.New("missing event_id")
	case p.Type == "":
		return payload{}, errors.New("missing type")
	case p.ChargeID == "":
		return payload{}, errors.New("missing charge_id")
	case len(p.Data) == 0 || string(p.Data) == "null":
		return payload{}, errors.New("missing data")
	case !worker.IsKnownEventType(p.Type):
		return payload{}, fmt.Errorf("unknown event type %q", p.Type)
	}
	return p, nil
}

func (h *Handler) addMetric(ctx context.Context, tx pgx.Tx, name string, delta int64) error {
	err := h.store.AddTx(ctx, tx, store.Metric{
		Name:          name,
		WindowStart:   time.Now().UTC().Truncate(time.Minute),
		WindowSeconds: metricWindowSeconds,
		Count:         delta,
	})
	if err != nil {
		return fmt.Errorf("record metric %s: %w", name, err)
	}
	return nil
}

// recordRejectedSignature counts a rejected request. It uses its own
// transaction because a rejected request never reaches TX1, and stays
// best-effort so a metrics failure cannot change the HTTP response.
func (h *Handler) recordRejectedSignature(ctx context.Context) {
	err := h.store.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return h.addMetric(txCtx, tx, metricWebhooksRejectedSignature, 1)
	})
	if err != nil {
		middleware.LoggerFrom(ctx).ErrorContext(ctx, "record rejected signature metric", slog.Any("error", err))
	}
}

func readBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errBodyTooLarge
		}
		return nil, err
	}
	return body, nil
}

func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		middleware.LoggerFrom(ctx).ErrorContext(ctx, "write json response", slog.Any("error", err))
	}
}

func writeError(ctx context.Context, w http.ResponseWriter, status int, message string) {
	writeJSON(ctx, w, status, map[string]string{"error": message})
}
