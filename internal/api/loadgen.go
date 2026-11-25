package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

const loadgenMaxBody = 4 << 10

// loadgenRunDTO is one stored loadgen run as the dashboard reads it.
type loadgenRunDTO struct {
	ID              int64     `json:"id"`
	Route           string    `json:"route"`
	Mode            string    `json:"mode"`
	Seed            int64     `json:"seed"`
	Target          string    `json:"target"`
	Events          int       `json:"events"`
	Concurrency     int       `json:"concurrency"`
	Sent            int64     `json:"sent"`
	Accepted        int64     `json:"accepted"`
	Duplicates      int64     `json:"duplicates"`
	Rejected        int64     `json:"rejected"`
	ServerErrors    int64     `json:"server_errors"`
	Other           int64     `json:"other"`
	TransportErrors int64     `json:"transport_errors"`
	P50MS           float64   `json:"p50_ms"`
	P95MS           float64   `json:"p95_ms"`
	P99MS           float64   `json:"p99_ms"`
	AvgMS           float64   `json:"avg_ms"`
	DurationMS      float64   `json:"duration_ms"`
	CreatedAt       time.Time `json:"created_at"`
}

func newLoadgenRunDTO(run store.LoadgenRun) loadgenRunDTO {
	return loadgenRunDTO{
		ID:              run.ID,
		Route:           run.Route,
		Mode:            run.Mode,
		Seed:            run.Seed,
		Target:          run.Target,
		Events:          run.Events,
		Concurrency:     run.Concurrency,
		Sent:            run.Sent,
		Accepted:        run.Accepted,
		Duplicates:      run.Duplicates,
		Rejected:        run.Rejected,
		ServerErrors:    run.ServerErrors,
		Other:           run.Other,
		TransportErrors: run.TransportErrors,
		P50MS:           run.P50MS,
		P95MS:           run.P95MS,
		P99MS:           run.P99MS,
		AvgMS:           run.AvgMS,
		DurationMS:      run.DurationMS,
		CreatedAt:       run.CreatedAt,
	}
}

type listLoadgenRunsDTO struct {
	Items []loadgenRunDTO `json:"items"`
}

func newListLoadgenRunsDTO(runs []store.LoadgenRun) listLoadgenRunsDTO {
	items := make([]loadgenRunDTO, 0, len(runs))
	for _, run := range runs {
		items = append(items, newLoadgenRunDTO(run))
	}
	return listLoadgenRunsDTO{Items: items}
}

// loadgenInsert persists a run. It is a function so the endpoint can be tested
// without a database.
type loadgenInsert func(context.Context, store.LoadgenRun) (store.LoadgenRun, error)

// loadgenIngest accepts one loadgen result and stores it. It is registered only
// when the service runs with LOADGEN_INGEST=true, so the read-only dashboard
// keeps a single explicit, gated write path next to the demo endpoint.
type loadgenIngest struct {
	insert loadgenInsert
	logger *slog.Logger
}

func newLoadgenIngest(insert loadgenInsert, logger *slog.Logger) *loadgenIngest {
	return &loadgenIngest{insert: insert, logger: logger}
}

type loadgenRunRequest struct {
	Route           string  `json:"route"`
	Mode            string  `json:"mode"`
	Seed            int64   `json:"seed"`
	Target          string  `json:"target"`
	Events          int     `json:"events"`
	Concurrency     int     `json:"concurrency"`
	Sent            int64   `json:"sent"`
	Accepted        int64   `json:"accepted"`
	Duplicates      int64   `json:"duplicates"`
	Rejected        int64   `json:"rejected"`
	ServerErrors    int64   `json:"server_errors"`
	Other           int64   `json:"other"`
	TransportErrors int64   `json:"transport_errors"`
	P50MS           float64 `json:"p50_ms"`
	P95MS           float64 `json:"p95_ms"`
	P99MS           float64 `json:"p99_ms"`
	AvgMS           float64 `json:"avg_ms"`
	DurationMS      float64 `json:"duration_ms"`
}

func (l *loadgenIngest) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		l.fail(w, http.StatusForbidden, "forbidden_origin", "loadgen ingest only accepts same-origin requests")
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		l.fail(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, loadgenMaxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request loadgenRunRequest
	if err := decoder.Decode(&request); err != nil {
		l.fail(w, http.StatusBadRequest, "invalid_body", "body must be a loadgen run report")
		return
	}
	if err := request.validate(); err != nil {
		l.fail(w, http.StatusBadRequest, "invalid_run", err.Error())
		return
	}

	stored, err := l.insert(r.Context(), request.toStore())
	if err != nil {
		l.logger.ErrorContext(r.Context(), "insert loadgen run", slog.Any("error", err))
		l.fail(w, http.StatusInternalServerError, "internal_error", "unexpected error")
		return
	}
	l.respond(w, http.StatusCreated, newLoadgenRunDTO(stored))
}

func (req loadgenRunRequest) validate() error {
	switch req.Route {
	case "guarded", "naive":
	default:
		return fmt.Errorf("route must be guarded or naive")
	}
	switch req.Mode {
	case "normal", "duplicate", "out-of-order":
	default:
		return fmt.Errorf("mode must be normal, duplicate or out-of-order")
	}
	for _, check := range []struct {
		name  string
		value int64
	}{
		{"events", int64(req.Events)},
		{"concurrency", int64(req.Concurrency)},
		{"sent", req.Sent},
		{"accepted", req.Accepted},
		{"duplicates", req.Duplicates},
		{"rejected", req.Rejected},
		{"server_errors", req.ServerErrors},
		{"other", req.Other},
		{"transport_errors", req.TransportErrors},
	} {
		if check.value < 0 {
			return fmt.Errorf("%s must be non-negative", check.name)
		}
	}
	for _, check := range []struct {
		name  string
		value float64
	}{
		{"p50_ms", req.P50MS},
		{"p95_ms", req.P95MS},
		{"p99_ms", req.P99MS},
		{"avg_ms", req.AvgMS},
		{"duration_ms", req.DurationMS},
	} {
		if check.value < 0 {
			return fmt.Errorf("%s must be non-negative", check.name)
		}
	}
	return nil
}

func (req loadgenRunRequest) toStore() store.LoadgenRun {
	return store.LoadgenRun{
		Route:           req.Route,
		Mode:            req.Mode,
		Seed:            req.Seed,
		Target:          req.Target,
		Events:          req.Events,
		Concurrency:     req.Concurrency,
		Sent:            req.Sent,
		Accepted:        req.Accepted,
		Duplicates:      req.Duplicates,
		Rejected:        req.Rejected,
		ServerErrors:    req.ServerErrors,
		Other:           req.Other,
		TransportErrors: req.TransportErrors,
		P50MS:           req.P50MS,
		P95MS:           req.P95MS,
		P99MS:           req.P99MS,
		AvgMS:           req.AvgMS,
		DurationMS:      req.DurationMS,
	}
}

func (h *Handler) listLoadgenRuns(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		h.failParam(w, err)
		return
	}
	runs, err := h.store.ListLoadgenRuns(r.Context(), limit)
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	h.respond(w, http.StatusOK, newListLoadgenRunsDTO(runs))
}

func (h *Handler) latestLoadgenRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.LatestLoadgenRuns(r.Context())
	if err != nil {
		h.failInternal(w, r, err)
		return
	}
	h.respond(w, http.StatusOK, newListLoadgenRunsDTO(runs))
}

func (l *loadgenIngest) respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		l.logger.Error("write loadgen response", slog.Any("error", err))
	}
}

func (l *loadgenIngest) fail(w http.ResponseWriter, status int, code, message string) {
	l.respond(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
