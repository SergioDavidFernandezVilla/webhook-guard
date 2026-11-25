package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/sergiodavidfv/webhook-guard/internal/signature"
)

const (
	demoRateLimit  = 20
	demoRateWindow = time.Minute
	demoMaxBody    = 1 << 10
	demoCurrency   = "USD"
)

// demoEndpoint sends signed sample webhooks through the real ingestion handler.
// It is a deliberately narrow write path: the client only picks a scenario,
// never the payload, and the signing secret stays in this process.
type demoEndpoint struct {
	webhook http.Handler
	secret  []byte
	logger  *slog.Logger
	now     func() time.Time

	mu          sync.Mutex
	windowStart time.Time
	windowCount int
}

func newDemoEndpoint(webhook http.Handler, secret []byte, logger *slog.Logger, now func() time.Time) *demoEndpoint {
	return &demoEndpoint{webhook: webhook, secret: secret, logger: logger, now: now}
}

type demoRequest struct {
	Scenario string `json:"scenario"`
}

type demoStep struct {
	HTTPStatus int    `json:"http_status"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

type demoResponse struct {
	Scenario string     `json:"scenario"`
	EventID  string     `json:"event_id"`
	ChargeID string     `json:"charge_id"`
	Steps    []demoStep `json:"steps"`
}

type demoEvent struct {
	EventID  string   `json:"event_id"`
	Type     string   `json:"type"`
	ChargeID string   `json:"charge_id"`
	Data     demoData `json:"data"`
}

type demoData struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func (d *demoEndpoint) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		d.fail(w, http.StatusForbidden, "forbidden_origin", "demo endpoint only accepts same-origin requests")
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		d.fail(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, demoMaxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request demoRequest
	if err := decoder.Decode(&request); err != nil {
		d.fail(w, http.StatusBadRequest, "invalid_body", "body must be {\"scenario\":\"...\"}")
		return
	}

	if !d.allow() {
		d.fail(w, http.StatusTooManyRequests, "rate_limited", "too many demo requests, try again in a minute")
		return
	}

	var response demoResponse
	switch request.Scenario {
	case "accepted":
		response = d.runSingle(r.Context(), "payment.authorized", 1000, d.secret, d.now().Unix())
	case "invalid_transition":
		response = d.runSingle(r.Context(), "payment.refunded", 500, d.secret, d.now().Unix())
	case "duplicate":
		response = d.runDuplicate(r.Context())
	case "invalid_signature":
		response = d.runSingle(r.Context(), "payment.authorized", 1000, []byte("whsec_not_the_secret"), d.now().Unix())
	case "expired":
		response = d.runSingle(r.Context(), "payment.authorized", 1000, d.secret, d.now().Add(-time.Hour).Unix())
	default:
		d.fail(w, http.StatusBadRequest, "unknown_scenario",
			"scenario must be accepted, duplicate, invalid_signature, expired or invalid_transition")
		return
	}

	response.Scenario = request.Scenario
	d.respond(w, http.StatusOK, response)
}

func (d *demoEndpoint) allow() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	if d.windowStart.IsZero() || now.Sub(d.windowStart) >= demoRateWindow {
		d.windowStart = now
		d.windowCount = 0
	}
	if d.windowCount >= demoRateLimit {
		return false
	}
	d.windowCount++
	return true
}

func (d *demoEndpoint) runSingle(ctx context.Context, eventType string, amount int64, secret []byte, timestamp int64) demoResponse {
	suffix := randomSuffix()
	ev := demoEvent{
		EventID:  "evt_demo_" + suffix,
		Type:     eventType,
		ChargeID: "ch_demo_" + suffix,
		Data:     demoData{AmountMinor: amount, Currency: demoCurrency},
	}
	body, err := json.Marshal(ev)
	if err != nil {
		d.logger.Error("marshal demo event", slog.Any("error", err))
		return demoResponse{EventID: ev.EventID, ChargeID: ev.ChargeID, Steps: []demoStep{{Error: "internal_error"}}}
	}
	return demoResponse{
		EventID:  ev.EventID,
		ChargeID: ev.ChargeID,
		Steps:    []demoStep{d.deliver(ctx, secret, timestamp, body)},
	}
}

func (d *demoEndpoint) runDuplicate(ctx context.Context) demoResponse {
	suffix := randomSuffix()
	ev := demoEvent{
		EventID:  "evt_demo_" + suffix,
		Type:     "payment.authorized",
		ChargeID: "ch_demo_" + suffix,
		Data:     demoData{AmountMinor: 1000, Currency: demoCurrency},
	}
	body, err := json.Marshal(ev)
	if err != nil {
		d.logger.Error("marshal demo event", slog.Any("error", err))
		return demoResponse{EventID: ev.EventID, ChargeID: ev.ChargeID, Steps: []demoStep{{Error: "internal_error"}}}
	}

	steps := make([]demoStep, 0, 2)
	for i := 0; i < 2; i++ {
		steps = append(steps, d.deliver(ctx, d.secret, d.now().Unix(), body))
	}
	return demoResponse{EventID: ev.EventID, ChargeID: ev.ChargeID, Steps: steps}
}

func (d *demoEndpoint) deliver(ctx context.Context, secret []byte, timestamp int64, body []byte) demoStep {
	sig := signature.Sign(secret, timestamp, body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/webhooks/payments", bytes.NewReader(body))
	if err != nil {
		d.logger.Error("build demo request", slog.Any("error", err))
		return demoStep{Error: "internal_error"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", fmt.Sprintf("t=%d,v1=%x", timestamp, sig))

	recorder := &captureResponse{header: make(http.Header)}
	d.webhook.ServeHTTP(recorder, req)

	step := demoStep{HTTPStatus: recorder.statusCode()}
	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(recorder.body.Bytes(), &payload); err == nil {
		step.Status = payload.Status
		step.Error = payload.Error
	}
	return step
}

type captureResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *captureResponse) Header() http.Header {
	return c.header
}

func (c *captureResponse) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *captureResponse) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

func (c *captureResponse) statusCode() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

func sameOrigin(r *http.Request) bool {
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "", "same-origin", "none":
	default:
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	return parsed.Host == host
}

func randomSuffix() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

func (d *demoEndpoint) respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		d.logger.Error("write demo response", slog.Any("error", err))
	}
}

func (d *demoEndpoint) fail(w http.ResponseWriter, status int, code, message string) {
	d.respond(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
