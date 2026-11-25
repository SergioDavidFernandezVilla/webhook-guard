package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

func newTestIngest(t *testing.T) (*loadgenIngest, *store.LoadgenRun) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var saved store.LoadgenRun
	insert := func(_ context.Context, run store.LoadgenRun) (store.LoadgenRun, error) {
		run.ID = 42
		run.CreatedAt = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
		saved = run
		return run, nil
	}
	return newLoadgenIngest(insert, logger), &saved
}

func newIngestRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/loadgen/runs", strings.NewReader(body))
	r.Host = "localhost:4321"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:4321")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

const validRunBody = `{
	"route":"guarded","mode":"duplicate","seed":3000,"target":"http://app:8080",
	"events":500,"concurrency":20,"sent":1000,"accepted":500,"duplicates":500,
	"rejected":0,"server_errors":0,"other":0,"transport_errors":0,
	"p50_ms":35.9,"p95_ms":285.7,"p99_ms":531.1,"avg_ms":60.2,"duration_ms":4200
}`

func TestLoadgenIngest_StoresRun(t *testing.T) {
	endpoint, saved := newTestIngest(t)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newIngestRequest(t, validRunBody))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if saved.Route != "guarded" || saved.Duplicates != 500 || saved.P50MS != 35.9 {
		t.Fatalf("saved run = %+v, want guarded/500/35.9", saved)
	}

	var response loadgenRunDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Route != "guarded" {
		t.Errorf("response = %+v, want id 42 and route guarded", response)
	}
}

func TestLoadgenIngest_RejectsUnknownRoute(t *testing.T) {
	endpoint, _ := newTestIngest(t)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newIngestRequest(t, `{"route":"production","mode":"normal"}`))

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "route") {
		t.Fatalf("status = %d body = %s, want 400 with route error", rec.Code, rec.Body.String())
	}
}

func TestLoadgenIngest_RejectsNegativeCounter(t *testing.T) {
	endpoint, _ := newTestIngest(t)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newIngestRequest(t, `{"route":"guarded","mode":"normal","sent":-1}`))

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sent") {
		t.Fatalf("status = %d body = %s, want 400 with sent error", rec.Code, rec.Body.String())
	}
}

func TestLoadgenIngest_RejectsUnknownField(t *testing.T) {
	endpoint, _ := newTestIngest(t)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newIngestRequest(t, `{"route":"guarded","mode":"normal","sql":"drop table"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestLoadgenIngest_CrossOriginIsRejected(t *testing.T) {
	endpoint, _ := newTestIngest(t)

	r := newIngestRequest(t, validRunBody)
	r.Header.Set("Origin", "http://evil.example")

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestLoadgenIngest_WrongContentTypeIsRejected(t *testing.T) {
	endpoint, _ := newTestIngest(t)

	r := newIngestRequest(t, validRunBody)
	r.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, r)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestLoadgenIngest_InsertErrorIs500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	endpoint := newLoadgenIngest(func(context.Context, store.LoadgenRun) (store.LoadgenRun, error) {
		return store.LoadgenRun{}, errors.New("boom")
	}, logger)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newIngestRequest(t, validRunBody))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("body leaks the internal error: %s", rec.Body.String())
	}
}
