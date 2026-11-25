//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://webhook:webhook@localhost:5432/webhook_guard?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := store.Open(ctx, dsn, store.OpenOptions{})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func newTestRouter(t *testing.T, s *store.Store) http.Handler {
	t.Helper()
	handler := New(s, Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		r.NotFound(handler.NotFound)
		r.MethodNotAllowed(handler.MethodNotAllowed)
		handler.Register(r)
	})
	return router
}

func get(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %T: %v (body=%s)", value, err, rec.Body.String())
	}
	return value
}

func TestDashboardAPI_ReadEndpoints(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	chargeID := "ch_api_" + suffix
	eventID := "evt_api_" + suffix

	if err := s.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		if err := s.UpsertTx(txCtx, tx, store.Charge{
			ChargeID:    chargeID,
			State:       store.StateAuthorized,
			AmountMinor: 1234,
			Currency:    "USD",
			LastEventID: eventID,
		}); err != nil {
			return err
		}
		_, _, err := s.InsertEventTx(txCtx, tx, store.Event{
			EventID:            eventID,
			Type:               "payment.authorized",
			ChargeID:           chargeID,
			Payload:            json.RawMessage(`{"event_id":"` + eventID + `"}`),
			SignatureTimestamp: time.Now().Unix(),
			CorrelationID:      "itest",
		})
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := s.Pool().Exec(cleanupCtx, `DELETE FROM webhook_events WHERE event_id = $1`, eventID); err != nil {
			t.Errorf("cleanup event: %v", err)
		}
		if _, err := s.Pool().Exec(cleanupCtx, `DELETE FROM charges WHERE charge_id = $1`, chargeID); err != nil {
			t.Errorf("cleanup charge: %v", err)
		}
	})

	router := newTestRouter(t, s)

	t.Run("overview", func(t *testing.T) {
		rec := get(t, router, "/api/v1/overview")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		body := decode[overviewDTO](t, rec)
		if !body.Database.Ready {
			t.Errorf("database.ready = false, want true")
		}
		if body.Charges.Total < 1 {
			t.Errorf("charges.total = %d, want at least 1", body.Charges.Total)
		}
		if _, ok := body.Charges.ByState[string(store.StateAuthorized)]; !ok {
			t.Errorf("charges.by_state missing authorized key: %+v", body.Charges.ByState)
		}
	})

	t.Run("capabilities", func(t *testing.T) {
		rec := get(t, router, "/api/v1/capabilities")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := decode[capabilitiesDTO](t, rec)
		if body.DemoEnabled {
			t.Error("demo_enabled = true, want false")
		}
	})

	t.Run("charges list", func(t *testing.T) {
		rec := get(t, router, "/api/v1/charges?q="+chargeID+"&limit=5")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		body := decode[listChargesDTO](t, rec)
		if body.Total != 1 {
			t.Errorf("total = %d, want 1", body.Total)
		}
		if len(body.Items) != 1 || body.Items[0].ChargeID != chargeID {
			t.Errorf("items = %+v, want the seeded charge", body.Items)
		}
	})

	t.Run("charge detail", func(t *testing.T) {
		rec := get(t, router, "/api/v1/charges/"+chargeID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		body := decode[chargeDetailDTO](t, rec)
		if body.Charge.ChargeID != chargeID {
			t.Errorf("charge_id = %q, want %q", body.Charge.ChargeID, chargeID)
		}
		found := false
		for _, ev := range body.Events {
			if ev.EventID == eventID {
				found = true
			}
		}
		if !found {
			t.Errorf("events = %+v, want the seeded event", body.Events)
		}
	})

	t.Run("missing charge", func(t *testing.T) {
		rec := get(t, router, "/api/v1/charges/ch_missing_"+suffix)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		body := decode[errorBody](t, rec)
		if body.Error.Code != "not_found" {
			t.Errorf("code = %q, want not_found", body.Error.Code)
		}
	})

	t.Run("events list", func(t *testing.T) {
		rec := get(t, router, "/api/v1/events?charge_id="+chargeID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		body := decode[listEventsDTO](t, rec)
		if len(body.Items) != 1 || body.Items[0].EventID != eventID {
			t.Errorf("items = %+v, want the seeded event", body.Items)
		}
	})

	t.Run("event detail", func(t *testing.T) {
		rec := get(t, router, "/api/v1/events/"+eventID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		body := decode[eventDetailResponse](t, rec)
		if body.Event.EventID != eventID {
			t.Errorf("event_id = %q, want %q", body.Event.EventID, eventID)
		}
		if body.Ledger != nil {
			t.Errorf("ledger = %+v, want nil for an unapplied event", body.Ledger)
		}
	})

	t.Run("missing event", func(t *testing.T) {
		rec := get(t, router, "/api/v1/events/evt_missing_"+suffix)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		body := decode[errorBody](t, rec)
		if body.Error.Code != "not_found" {
			t.Errorf("code = %q, want not_found", body.Error.Code)
		}
	})

	t.Run("invalid state", func(t *testing.T) {
		rec := get(t, router, "/api/v1/charges?state=refund")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		body := decode[errorBody](t, rec)
		if body.Error.Code != "invalid_parameter" {
			t.Errorf("code = %q, want invalid_parameter", body.Error.Code)
		}
	})

	t.Run("late events", func(t *testing.T) {
		rec := get(t, router, "/api/v1/late-events?limit=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		_ = decode[listLateEventsDTO](t, rec)
	})

	t.Run("metrics", func(t *testing.T) {
		rec := get(t, router, "/api/v1/metrics?minutes=5")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		_ = decode[struct {
			Items []metricPointDTO `json:"items"`
		}](t, rec)
	})

	t.Run("latest report", func(t *testing.T) {
		rec := get(t, router, "/api/v1/reconciliation/latest")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		_ = decode[latestReportResponse](t, rec)
	})

	t.Run("report history", func(t *testing.T) {
		rec := get(t, router, "/api/v1/reconciliation/reports?limit=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		_ = decode[listReportsDTO](t, rec)
	})

	t.Run("unknown route", func(t *testing.T) {
		rec := get(t, router, "/api/v1/does-not-exist")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		body := decode[errorBody](t, rec)
		if body.Error.Code != "not_found" {
			t.Errorf("code = %q, want not_found", body.Error.Code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/overview", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
		body := decode[errorBody](t, rec)
		if body.Error.Code != "method_not_allowed" {
			t.Errorf("code = %q, want method_not_allowed", body.Error.Code)
		}
	})
}

func TestDashboardAPI_DemoDisabled(t *testing.T) {
	s := testStore(t)
	router := newTestRouter(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/webhooks", nil)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when demo is off", rec.Code)
	}
}

func TestDashboardAPI_LoadgenRuns(t *testing.T) {
	s := testStore(t)
	handler := New(s, Options{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		LoadgenIngest: true,
	})
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		r.NotFound(handler.NotFound)
		r.MethodNotAllowed(handler.MethodNotAllowed)
		handler.Register(r)
	})

	body := `{"route":"naive","mode":"duplicate","seed":9001,"target":"http://app-naive:8080",` +
		`"events":10,"concurrency":2,"sent":20,"accepted":10,"duplicates":0,"rejected":0,` +
		`"server_errors":10,"other":0,"transport_errors":0,"p50_ms":1.5,"p95_ms":2.5,"p99_ms":3.5,` +
		`"avg_ms":2.0,"duration_ms":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/loadgen/runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	created := decode[loadgenRunDTO](t, rec)
	if created.ID == 0 || created.Route != "naive" || created.ServerErrors != 10 {
		t.Fatalf("created run = %+v, want id, route naive and 10 server errors", created)
	}
	t.Cleanup(func() {
		if _, err := s.Pool().Exec(context.Background(),
			`DELETE FROM loadgen_runs WHERE id = $1`, created.ID); err != nil {
			t.Errorf("cleanup loadgen run: %v", err)
		}
	})

	latest := decode[listLoadgenRunsDTO](t, get(t, router, "/api/v1/loadgen/latest"))
	found := false
	for _, run := range latest.Items {
		if run.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("latest runs do not include the inserted run %d", created.ID)
	}

	runs := decode[listLoadgenRunsDTO](t, get(t, router, "/api/v1/loadgen/runs?limit=5"))
	if len(runs.Items) == 0 || runs.Items[0].ID != created.ID {
		t.Errorf("list runs = %+v, want the inserted run first", runs.Items)
	}
}
