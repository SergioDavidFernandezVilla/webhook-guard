// Command webhook-guard runs the webhook ingestion service and its operational
// subcommands.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/sergiodavidfv/webhook-guard/internal/api"
	"github.com/sergiodavidfv/webhook-guard/internal/config"
	"github.com/sergiodavidfv/webhook-guard/internal/guard"
	"github.com/sergiodavidfv/webhook-guard/internal/handler"
	"github.com/sergiodavidfv/webhook-guard/internal/middleware"
	"github.com/sergiodavidfv/webhook-guard/internal/reconcile"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
	"github.com/sergiodavidfv/webhook-guard/internal/worker"
)

const shutdownTimeout = 10 * time.Second

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "serve":
		if err := serve(ctx, stdout); err != nil {
			_, _ = fmt.Fprintf(stderr, "serve: %v\n", err)
			return 1
		}
		return 0
	case "replay":
		return runReplay(ctx, args[1:], stdout, stderr)
	case "reconcile":
		return runReconcile(ctx, args[1:], stdout, stderr)
	case "report":
		return runReport(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func serve(parent context.Context, stdout io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg, stdout)
	pool, err := store.Open(parent, cfg.DatabaseURL, store.OpenOptions{ConnectTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer pool.Close()

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	riverDriver, err := riverDriverFor(ctx, pool)
	if err != nil {
		return err
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, worker.NewWebhookWorker(pool, guard.New(pool), logger))
	riverClient, err := river.NewClient(riverDriver, &river.Config{
		Logger: logger,
		Queues: map[string]river.QueueConfig{
			cfg.WorkerQueue: {MaxWorkers: 10},
		},
		Workers: workers,
	})
	if err != nil {
		return fmt.Errorf("create river client: %w", err)
	}
	if err := riverClient.Start(ctx); err != nil {
		return fmt.Errorf("start river: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := riverClient.Stop(stopCtx); err != nil {
			logger.Error("stop river", slog.Any("error", err))
		}
	}()

	webhookHandler := handler.New(
		pool,
		guard.New(pool),
		riverClient,
		[]byte(cfg.WebhookSigningSecret),
		cfg.SignatureTolerance,
		cfg.WorkerQueue,
	)

	dashboardAPI := api.New(pool, api.Options{
		DemoMode:      cfg.DemoMode,
		DemoSecret:    []byte(cfg.WebhookSigningSecret),
		Webhook:       webhookHandler,
		LoadgenIngest: cfg.LoadgenIngest,
		Logger:        logger,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newRouter(logger, pool, webhookHandler, dashboardAPI),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", slog.String("addr", cfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("server stopped")
	return nil
}

func riverDriverFor(ctx context.Context, st *store.Store) (*riverpgxv5.Driver, error) {
	driver := riverpgxv5.New(st.Pool())
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		return nil, fmt.Errorf("create river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("migrate river schema: %w", err)
	}
	return driver, nil
}

func openStore(ctx context.Context, stderr io.Writer) (*store.Store, *slog.Logger, int) {
	cfg, err := config.LoadWithoutSecret()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "config: %v\n", err)
		return nil, nil, 1
	}
	logger := newLogger(cfg, stderr)
	st, err := store.Open(ctx, cfg.DatabaseURL, store.OpenOptions{ConnectTimeout: 5 * time.Second})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open store: %v\n", err)
		return nil, nil, 1
	}
	return st, logger, 0
}

func runReplay(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fromFlag := fs.String("from", "", "start of window: RFC3339 or unix seconds (default: epoch)")
	toFlag := fs.String("to", "", "end of window: RFC3339 or unix seconds (default: now)")
	dryRun := fs.Bool("dry-run", false, "only count pending events")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	from, err := parseTimestamp(*fromFlag, time.Unix(0, 0).UTC())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 2
	}
	to, err := parseTimestamp(*toFlag, time.Now().Add(time.Second))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 2
	}

	st, logger, code := openStore(ctx, stderr)
	if code != 0 {
		return code
	}
	defer st.Close()

	driver, err := riverDriverFor(ctx, st)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 1
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, worker.NewWebhookWorker(st, guard.New(st), logger))
	client, err := river.NewClient(driver, &river.Config{Logger: logger, Workers: workers})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 1
	}

	// Jobs are enqueued on the queue the running service consumes; the default
	// is the same as the guarded path. A naive comparison must point
	// WORKER_QUEUE at the queue of the app under test.
	cfg, err := config.LoadWithoutSecret()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 1
	}
	queue := cfg.WorkerQueue
	if queue == "" {
		queue = river.QueueDefault
	}
	insertOpts := &river.InsertOpts{Queue: queue}

	enqueue := func(ctx context.Context, tx pgx.Tx, eventID string) error {
		_, insertErr := client.InsertTx(ctx, tx, worker.WebhookArgs{EventID: eventID}, insertOpts)
		return insertErr
	}
	count, err := reconcile.Replay(ctx, st, enqueue, reconcile.ReplayOptions{From: from, To: to, DryRun: *dryRun})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 1
	}

	result := map[string]any{
		"dry_run":  *dryRun,
		"from":     from.UTC().Format(time.RFC3339),
		"to":       to.UTC().Format(time.RFC3339),
		"events":   count,
		"enqueued": 0,
	}
	if !*dryRun {
		result["enqueued"] = count
	}
	if err := printJSON(stdout, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "replay: %v\n", err)
		return 1
	}
	return 0
}

func runReconcile(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	exportPath := fs.String("export", "-", "export file path, or - for stdin")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	var reader io.Reader
	if *exportPath == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(*exportPath)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
			return 1
		}
		defer func() { _ = file.Close() }()
		reader = file
	}

	st, _, code := openStore(ctx, stderr)
	if code != 0 {
		return code
	}
	defer st.Close()

	exportID, err := reconcile.Import(ctx, st, reader)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
		return 1
	}
	report, err := reconcile.Diff(ctx, st, exportID)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
		return 1
	}
	lateResolved, err := reconcile.ResolveLateEvents(ctx, st)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
		return 1
	}

	raw, err := json.Marshal(report)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
		return 1
	}
	if _, err := st.InsertReport(ctx, report.BatchID, raw); err != nil {
		_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
		return 1
	}
	if err := printJSON(stdout, map[string]any{
		"report":               report,
		"late_events_resolved": lateResolved,
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "reconcile: %v\n", err)
		return 1
	}
	return 0
}

func runReport(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	last := fs.Bool("last", false, "print the most recently persisted report")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !*last {
		_, _ = fmt.Fprintln(stderr, "report: --last is required")
		fs.Usage()
		return 2
	}

	st, _, code := openStore(ctx, stderr)
	if code != 0 {
		return code
	}
	defer st.Close()

	_, raw, _, err := st.LatestReport(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_, _ = fmt.Fprintln(stderr, "report: no persisted reports")
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "report: %v\n", err)
		return 1
	}

	var report reconcile.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		_, _ = fmt.Fprintf(stderr, "report: %v\n", err)
		return 1
	}
	if err := printJSON(stdout, raw); err != nil {
		_, _ = fmt.Fprintf(stderr, "report: %v\n", err)
		return 1
	}
	if report.HasDiffs() {
		return 3
	}
	return 0
}

func parseTimestamp(value string, fallback time.Time) (time.Time, error) {
	if value == "" {
		return fallback, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q: use RFC3339 or unix seconds", value)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func printJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func newLogger(cfg config.Config, out io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(out, opts))
	}
	return slog.New(slog.NewJSONHandler(out, opts))
}

func newRouter(logger *slog.Logger, st *store.Store, webhookHandler http.Handler, dashboardAPI *api.Handler) http.Handler {
	router := chi.NewRouter()
	router.Use(chimw.RequestID)
	router.Use(middleware.Logging(logger))
	router.Use(middleware.Recoverer(logger))

	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
	})

	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(r.Context()); err != nil {
			middleware.LoggerFrom(r.Context()).WarnContext(r.Context(), "database not ready", slog.Any("error", err))
			writeJSON(r.Context(), w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
			return
		}
		writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ready"})
	})

	router.Post("/webhooks/payments", webhookHandler.ServeHTTP)

	router.Route("/api/v1", func(r chi.Router) {
		r.NotFound(dashboardAPI.NotFound)
		r.MethodNotAllowed(dashboardAPI.MethodNotAllowed)
		dashboardAPI.Register(r)
	})

	return router
}

func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		middleware.LoggerFrom(ctx).ErrorContext(ctx, "write json response", slog.Any("error", err))
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: webhook-guard <serve|replay|reconcile|report>")
}
