package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRun_NormalizesTrailingSlashTarget(t *testing.T) {
	opts := options{
		target: "http://localhost:8080/",
		secret: "whsec_test",
	}
	if got := strings.TrimRight(opts.target, "/"); got != "http://localhost:8080" {
		t.Errorf("target = %q, want %q", got, "http://localhost:8080")
	}
}

func TestSend_NonJSONBodyIsNotAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	client := &http.Client{}
	ev := makeEvent(1, 0, "authorized")
	o := send(context.Background(), client, options{secret: "whsec_test", target: server.URL}, ev)

	if o.err != nil {
		t.Fatalf("send() error = %v", o.err)
	}
	if !o.badPayload {
		t.Error("badPayload = false, want true for a non-JSON 200 body")
	}

	s := &stats{codes: make(map[int]int64)}
	s.record(o)
	if s.accepted != 0 || s.other != 1 {
		t.Errorf("accepted = %d, other = %d; want 0/1", s.accepted, s.other)
	}
}

func TestSend_ParsesDuplicateStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "duplicate"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &http.Client{}
	ev := makeEvent(1, 0, "authorized")
	o := send(context.Background(), client, options{secret: "whsec_test", target: server.URL}, ev)

	if o.err != nil || o.badPayload {
		t.Fatalf("send() err=%v badPayload=%v; want clean outcome", o.err, o.badPayload)
	}

	s := &stats{codes: make(map[int]int64)}
	s.record(o)
	if s.duplicates != 1 || s.accepted != 0 {
		t.Errorf("duplicates = %d, accepted = %d; want 1/0", s.duplicates, s.accepted)
	}
}

func TestSend_TimesOutOnSlowServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Millisecond}
	ev := makeEvent(1, 0, "authorized")
	o := send(context.Background(), client, options{secret: "whsec_test", target: server.URL}, ev)

	if o.err == nil {
		t.Fatal("send() error = nil, want timeout error")
	}

	s := &stats{codes: make(map[int]int64)}
	s.record(o)
	if s.transportErrors != 1 {
		t.Errorf("transportErrors = %d, want 1", s.transportErrors)
	}
}

func TestRun_RejectsUnknownRoute(t *testing.T) {
	err := run(options{
		target:      "http://localhost:8080",
		secret:      "whsec_test",
		mode:        modeNormal,
		events:      1,
		concurrency: 1,
		format:      formatText,
		route:       "production",
	})
	if err == nil || !strings.Contains(err.Error(), "route") {
		t.Fatalf("run() error = %v, want a route validation error", err)
	}
}

func TestPostResult_SendsPayload(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	result := report{
		Mode:           modeDuplicate,
		Seed:           7,
		Concurrency:    20,
		LogicalEvents:  500,
		Sent:           1000,
		Accepted:       500,
		Duplicates:     500,
		Target:         "http://app:8080",
		LatencyMillis:  map[string]float64{"p50": 1.5, "p95": 2.5, "p99": 3.5, "avg": 1.9},
		DurationMillis: 1234,
	}
	if err := postResult(context.Background(), server.Client(), options{route: "naive", resultURL: server.URL}, result); err != nil {
		t.Fatalf("postResult() error = %v", err)
	}

	if got["route"] != "naive" {
		t.Errorf("route = %v, want naive", got["route"])
	}
	if got["mode"] != modeDuplicate {
		t.Errorf("mode = %v, want %s", got["mode"], modeDuplicate)
	}
	if got["sent"] != float64(1000) || got["duplicates"] != float64(500) {
		t.Errorf("counters = %v/%v, want 1000/500", got["sent"], got["duplicates"])
	}
	if got["p50_ms"] != 1.5 {
		t.Errorf("p50_ms = %v, want 1.5", got["p50_ms"])
	}
}

func TestPostResult_Non2xxIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error"}}`))
	}))
	defer server.Close()

	err := postResult(context.Background(), server.Client(), options{route: "guarded", resultURL: server.URL}, report{})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("postResult() error = %v, want a 500 error", err)
	}
}
