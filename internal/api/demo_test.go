package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sergiodavidfv/webhook-guard/internal/signature"
)

var testNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// fakeWebhook behaves like the real ingestion handler for the demo tests: it
// verifies the signature with the configured secret before answering.
type fakeWebhook struct {
	secret []byte
	now    time.Time
	calls  int
	header string
	body   []byte
}

func (f *fakeWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls++
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.body = body
	f.header = r.Header.Get("X-Signature")

	w.Header().Set("Content-Type", "application/json")
	if err := signature.Verify(f.secret, f.header, body, f.now, 5*time.Minute); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid signature"}`))
		return
	}
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

func newTestDemo(t *testing.T, secret []byte) (*demoEndpoint, *fakeWebhook) {
	t.Helper()
	fake := &fakeWebhook{secret: secret, now: testNow}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newDemoEndpoint(fake, secret, logger, func() time.Time { return testNow }), fake
}

func newDemoRequest(t *testing.T, scenario string) *http.Request {
	t.Helper()
	body := `{"scenario":"` + scenario + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/demo/webhooks", strings.NewReader(body))
	r.Host = "localhost:4321"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:4321")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

func TestDemo_AcceptedSignsAndDelivers(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, fake := newTestDemo(t, secret)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newDemoRequest(t, "accepted"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var response demoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Steps) != 1 || response.Steps[0].Status != "accepted" || response.Steps[0].HTTPStatus != 200 {
		t.Fatalf("steps = %+v, want one accepted 200", response.Steps)
	}
	if fake.calls != 1 {
		t.Fatalf("webhook calls = %d, want 1", fake.calls)
	}
	if err := signature.Verify(secret, fake.header, fake.body, testNow, 5*time.Minute); err != nil {
		t.Errorf("demo signature does not verify with the real secret: %v", err)
	}
	if !strings.HasPrefix(response.EventID, "evt_demo_") || !strings.HasPrefix(response.ChargeID, "ch_demo_") {
		t.Errorf("demo ids = %q/%q, want evt_demo_/ch_demo_ prefixes", response.EventID, response.ChargeID)
	}
}

func TestDemo_DuplicateSendsTwice(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, fake := newTestDemo(t, secret)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newDemoRequest(t, "duplicate"))

	var response demoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(response.Steps))
	}
	if fake.calls != 2 {
		t.Fatalf("webhook calls = %d, want 2", fake.calls)
	}
}

func TestDemo_InvalidSignatureIsRejectedByRealPath(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, _ := newTestDemo(t, secret)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newDemoRequest(t, "invalid_signature"))

	var response demoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Steps) != 1 || response.Steps[0].HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("steps = %+v, want one 401 step", response.Steps)
	}
}

func TestDemo_ExpiredTimestampIsRejectedByRealPath(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, _ := newTestDemo(t, secret)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newDemoRequest(t, "expired"))

	var response demoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Steps) != 1 || response.Steps[0].HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("steps = %+v, want one 401 step", response.Steps)
	}
}

func TestDemo_UnknownScenarioIsRejected(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, _ := newTestDemo(t, secret)

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newDemoRequest(t, "drop_database"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown_scenario") {
		t.Errorf("body = %s, want unknown_scenario", rec.Body.String())
	}
}

func TestDemo_CrossOriginIsRejected(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, fake := newTestDemo(t, secret)

	r := newDemoRequest(t, "accepted")
	r.Header.Set("Origin", "http://evil.example")

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if fake.calls != 0 {
		t.Errorf("webhook calls = %d, want 0", fake.calls)
	}
}

func TestDemo_WrongContentTypeIsRejected(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, _ := newTestDemo(t, secret)

	r := newDemoRequest(t, "accepted")
	r.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, r)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestDemo_RateLimit(t *testing.T) {
	secret := []byte("whsec_demo_test")
	endpoint, _ := newTestDemo(t, secret)

	for i := 0; i < demoRateLimit; i++ {
		rec := httptest.NewRecorder()
		endpoint.serveHTTP(rec, newDemoRequest(t, "accepted"))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i+1, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	endpoint.serveHTTP(rec, newDemoRequest(t, "accepted"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status after limit = %d, want 429", rec.Code)
	}
}
