// Command loadgen sends signed webhook traffic to a running service. Every
// attempt is re-signed with a fresh timestamp so idempotency tests do not
// degrade into signature-tolerance tests.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	modeNormal      = "normal"
	modeDuplicate   = "duplicate"
	modeOutOfOrder  = "out-of-order"
	formatText      = "text"
	formatJSON      = "json"
	webhookPath     = "/webhooks/payments"
	defaultCurrency = "USD"
)

type options struct {
	target      string
	secret      string
	concurrency int
	events      int
	mode        string
	seed        int64
	format      string
	route       string
	resultURL   string
	timeout     time.Duration
}

type event struct {
	EventID  string `json:"event_id"`
	Type     string `json:"type"`
	ChargeID string `json:"charge_id"`
	Data     data   `json:"data"`
}

type data struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type item struct {
	events []event
}

type report struct {
	Target          string             `json:"target"`
	Mode            string             `json:"mode"`
	Seed            int64              `json:"seed"`
	Concurrency     int                `json:"concurrency"`
	LogicalEvents   int                `json:"logical_events"`
	Sent            int64              `json:"sent"`
	Accepted        int64              `json:"accepted"`
	Duplicates      int64              `json:"duplicates"`
	Rejected        int64              `json:"rejected"`
	ServerErrors    int64              `json:"server_errors"`
	Other           int64              `json:"other"`
	TransportErrors int64              `json:"transport_errors"`
	Codes           map[string]int64   `json:"http_codes"`
	LatencyMillis   map[string]float64 `json:"latency_ms"`
	DurationMillis  float64            `json:"duration_ms"`
}

type stats struct {
	mu              sync.Mutex
	sent            int64
	accepted        int64
	duplicates      int64
	rejected        int64
	serverErrors    int64
	other           int64
	transportErrors int64
	codes           map[int]int64
	latencies       []time.Duration
}

func main() {
	opts := parseFlags()
	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "loadgen: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.target, "target", os.Getenv("LOADGEN_TARGET"), "base URL of the service, e.g. http://app:8080")
	flag.StringVar(&opts.secret, "secret", os.Getenv("LOADGEN_WEBHOOK_SECRET"), "webhook signing secret (never logged)")
	flag.IntVar(&opts.concurrency, "concurrency", envInt("LOADGEN_CONCURRENCY", 10), "number of concurrent logical events in flight")
	flag.IntVar(&opts.events, "events", envInt("LOADGEN_EVENTS", 100), "number of logical events to generate")
	flag.StringVar(&opts.mode, "mode", envString("LOADGEN_MODE", modeNormal), "traffic mode: normal|duplicate|out-of-order")
	flag.Int64Var(&opts.seed, "seed", envInt64("LOADGEN_SEED", 1), "seed for reproducible event ids")
	flag.StringVar(&opts.format, "format", envString("LOADGEN_FORMAT", formatText), "output format: text|json")
	flag.StringVar(&opts.route, "route", envString("LOADGEN_ROUTE", "guarded"), "route label reported to the ingest endpoint: guarded|naive")
	flag.StringVar(&opts.resultURL, "result-url", os.Getenv("LOADGEN_RESULT_URL"), "optional URL that receives the run result (POST JSON)")
	flag.DurationVar(&opts.timeout, "timeout", 10*time.Second, "per-request timeout")
	flag.Parse()
	return opts
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func run(opts options) error {
	if opts.target == "" {
		return fmt.Errorf("--target or LOADGEN_TARGET is required")
	}
	opts.target = strings.TrimRight(opts.target, "/")
	if opts.secret == "" {
		return fmt.Errorf("--secret or LOADGEN_WEBHOOK_SECRET is required")
	}
	if opts.concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1")
	}
	if opts.events < 1 {
		return fmt.Errorf("--events must be at least 1")
	}
	switch opts.mode {
	case modeNormal, modeDuplicate, modeOutOfOrder:
	default:
		return fmt.Errorf("--mode must be normal, duplicate or out-of-order, got %q", opts.mode)
	}
	switch opts.format {
	case formatText, formatJSON:
	default:
		return fmt.Errorf("--format must be text or json, got %q", opts.format)
	}
	switch opts.route {
	case "guarded", "naive":
	default:
		return fmt.Errorf("--route must be guarded or naive, got %q", opts.route)
	}

	items := buildWorkload(opts)
	client := &http.Client{
		Timeout: opts.timeout,
		Transport: &http.Transport{
			MaxIdleConns:        opts.concurrency * 2,
			MaxIdleConnsPerHost: opts.concurrency * 2,
		},
	}

	ctx := context.Background()
	work := make(chan item)
	aggregate := &stats{codes: make(map[int]int64)}
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < opts.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range work {
				for _, ev := range it.events {
					aggregate.record(send(ctx, client, opts, ev))
				}
			}
		}()
	}
	for _, it := range items {
		work <- it
	}
	close(work)
	wg.Wait()

	result := aggregate.report(opts, len(items), time.Since(start))
	if err := output(result, opts.format); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if opts.resultURL != "" {
		if err := postResult(ctx, client, opts, result); err != nil {
			return fmt.Errorf("report result: %w", err)
		}
	}
	if result.TransportErrors > 0 {
		return fmt.Errorf("%d transport error(s)", result.TransportErrors)
	}
	return nil
}

func buildWorkload(opts options) []item {
	items := make([]item, 0, opts.events)
	switch opts.mode {
	case modeDuplicate:
		for i := 0; i < opts.events; i++ {
			ev := makeEvent(opts.seed, i, "authorized")
			items = append(items, item{events: []event{ev, ev}})
		}
	case modeOutOfOrder:
		for i := 0; i < opts.events; i++ {
			items = append(items, item{events: []event{
				makeEvent(opts.seed, i, "captured"),
				makeEvent(opts.seed, i, "authorized"),
			}})
		}
	default:
		for i := 0; i < opts.events; i++ {
			items = append(items, item{events: []event{makeEvent(opts.seed, i, "authorized")}})
		}
	}
	return items
}

func makeEvent(seed int64, index int, stage string) event {
	return event{
		EventID:  fmt.Sprintf("evt_load_%d_%d_%s", seed, index, stage),
		Type:     "payment." + stage,
		ChargeID: fmt.Sprintf("ch_load_%d_%d", seed, index),
		Data:     data{AmountMinor: 1000, Currency: defaultCurrency},
	}
}

type outcome struct {
	status     int
	kind       string
	badPayload bool
	latency    time.Duration
	err        error
}

func send(ctx context.Context, client *http.Client, opts options, ev event) outcome {
	body, err := json.Marshal(ev)
	if err != nil {
		return outcome{err: fmt.Errorf("marshal event: %w", err)}
	}

	timestamp := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(opts.secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.target+webhookPath, bytes.NewReader(body))
	if err != nil {
		return outcome{err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", fmt.Sprintf("t=%d,v1=%x", timestamp, mac.Sum(nil)))

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return outcome{latency: latency, err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return outcome{status: resp.StatusCode, latency: latency, err: readErr}
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		// A 2xx whose body does not parse is not an accepted event; flag it so
		// the tally never counts a broken response as accepted.
		return outcome{status: resp.StatusCode, badPayload: true, latency: latency}
	}

	return outcome{status: resp.StatusCode, kind: payload.Status, latency: latency}
}

func (s *stats) record(o outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sent++
	if o.err != nil {
		s.transportErrors++
		return
	}
	s.latencies = append(s.latencies, o.latency)
	s.codes[o.status]++

	switch {
	case o.badPayload:
		// Body did not parse: it is not an accepted event even on 200.
		s.other++
	case o.status == http.StatusOK && o.kind == "duplicate":
		s.duplicates++
	case o.status == http.StatusOK:
		s.accepted++
	case o.status >= http.StatusInternalServerError:
		s.serverErrors++
	case o.status >= http.StatusBadRequest:
		s.rejected++
	default:
		s.other++
	}
}

func (s *stats) report(opts options, logicalEvents int, elapsed time.Duration) report {
	s.mu.Lock()
	defer s.mu.Unlock()

	codes := make(map[string]int64, len(s.codes))
	for code, count := range s.codes {
		codes[strconv.Itoa(code)] = count
	}

	latency := percentiles(s.latencies)
	latency["avg"] = average(s.latencies)

	return report{
		Target:          opts.target,
		Mode:            opts.mode,
		Seed:            opts.seed,
		Concurrency:     opts.concurrency,
		LogicalEvents:   logicalEvents,
		Sent:            s.sent,
		Accepted:        s.accepted,
		Duplicates:      s.duplicates,
		Rejected:        s.rejected,
		ServerErrors:    s.serverErrors,
		Other:           s.other,
		TransportErrors: s.transportErrors,
		Codes:           codes,
		LatencyMillis:   latency,
		DurationMillis:  float64(elapsed.Microseconds()) / 1000,
	}
}

func percentiles(latencies []time.Duration) map[string]float64 {
	result := map[string]float64{"p50": 0, "p95": 0, "p99": 0}
	if len(latencies) == 0 {
		return result
	}
	sorted := append([]time.Duration(nil), latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	for _, p := range []struct {
		key   string
		value float64
	}{{"p50", 50}, {"p95", 95}, {"p99", 99}} {
		index := int(math.Ceil(p.value/100*float64(len(sorted)))) - 1
		index = max(0, min(index, len(sorted)-1))
		result[p.key] = float64(sorted[index].Microseconds()) / 1000
	}
	return result
}

func average(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, latency := range latencies {
		total += latency
	}
	return float64((total / time.Duration(len(latencies))).Microseconds()) / 1000
}

func output(result report, format string) error {
	if format == formatJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return fmt.Errorf("encode json report: %w", err)
		}
		return nil
	}

	fmt.Printf("target=%s mode=%s seed=%d concurrency=%d logical_events=%d\n",
		result.Target, result.Mode, result.Seed, result.Concurrency, result.LogicalEvents)
	fmt.Printf("sent=%d accepted=%d duplicates=%d rejected=%d server_errors=%d other=%d transport_errors=%d\n",
		result.Sent, result.Accepted, result.Duplicates, result.Rejected, result.ServerErrors, result.Other, result.TransportErrors)
	fmt.Printf("codes=%v\n", result.Codes)
	fmt.Printf("latency_ms: p50=%.2f p95=%.2f p99=%.2f avg=%.2f\n",
		result.LatencyMillis["p50"], result.LatencyMillis["p95"], result.LatencyMillis["p99"], result.LatencyMillis["avg"])
	fmt.Printf("duration_ms=%.2f\n", result.DurationMillis)
	return nil
}

// postResult sends the finished run to the service's gated ingest endpoint so
// the dashboard can show a real guarded-vs-naive comparison. The endpoint is
// opt-in (LOADGEN_INGEST=true); when no result URL is configured this is a
// no-op.
func postResult(ctx context.Context, client *http.Client, opts options, result report) error {
	payload := struct {
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
	}{
		Route:           opts.route,
		Mode:            result.Mode,
		Seed:            result.Seed,
		Target:          result.Target,
		Events:          result.LogicalEvents,
		Concurrency:     result.Concurrency,
		Sent:            result.Sent,
		Accepted:        result.Accepted,
		Duplicates:      result.Duplicates,
		Rejected:        result.Rejected,
		ServerErrors:    result.ServerErrors,
		Other:           result.Other,
		TransportErrors: result.TransportErrors,
		P50MS:           result.LatencyMillis["p50"],
		P95MS:           result.LatencyMillis["p95"],
		P99MS:           result.LatencyMillis["p99"],
		AvgMS:           result.LatencyMillis["avg"],
		DurationMS:      result.DurationMillis,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.resultURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ingest endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	return nil
}
