package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func TestLogging_AttachesCorrelationIDAndLogsStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	router := chi.NewRouter()
	router.Use(chimw.RequestID)
	router.Use(Logging(logger))
	router.Get("/ok", func(w http.ResponseWriter, r *http.Request) {
		LoggerFrom(r.Context()).Info("handler ran")
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	out := buf.String()
	for _, want := range []string{`"correlation_id"`, `"status":201`, `handler ran`} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q: %s", want, out)
		}
	}
}

func TestRecoverer_ReturnsJSON500WithoutLeakingPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	router := chi.NewRouter()
	router.Use(chimw.RequestID)
	router.Use(Logging(logger))
	router.Use(Recoverer(logger))
	router.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("secret internal detail")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "secret internal detail") {
		t.Errorf("response leaked panic detail: %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Errorf("panic was not logged: %s", buf.String())
	}
}

func TestLogging_FirstWriteHeaderWins(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	router := chi.NewRouter()
	router.Use(chimw.RequestID)
	router.Use(Logging(logger))
	router.Get("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.WriteHeader(http.StatusInternalServerError)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	if !strings.Contains(buf.String(), `"status":418`) {
		t.Errorf("log must keep the first WriteHeader status: %s", buf.String())
	}
}
