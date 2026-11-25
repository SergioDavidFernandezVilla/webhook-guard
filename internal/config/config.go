// Package config loads and validates runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr           = ":8080"
	defaultSignatureTolerance = 5 * time.Minute
	defaultLogLevel           = "info"
	defaultLogFormat          = "json"
	defaultWorkerQueue        = "default"
)

// Config holds every value the service reads at startup. It is populated only
// from environment variables so no secret ever lives in code.
type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	WebhookSigningSecret string
	SignatureTolerance   time.Duration
	LogLevel             slog.Level
	LogFormat            string
	WorkerQueue          string
	DemoMode             bool
	LoadgenIngest        bool
}

// Load reads the configuration for the HTTP service, which requires the
// signing secret and the worker queue.
func Load() (Config, error) {
	return load(true)
}

// LoadWithoutSecret reads the configuration for operational subcommands
// (replay, reconcile, report) that never verify webhook signatures and never
// run the HTTP server or the worker pool.
func LoadWithoutSecret() (Config, error) {
	return load(false)
}

func load(requireServiceConfig bool) (Config, error) {
	cfg := Config{
		LogFormat: strings.ToLower(getenv("LOG_FORMAT", defaultLogFormat)),
	}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	if requireServiceConfig {
		cfg.HTTPAddr = getenv("HTTP_ADDR", defaultHTTPAddr)
		cfg.WebhookSigningSecret = os.Getenv("WEBHOOK_SIGNING_SECRET")
		if cfg.WebhookSigningSecret == "" {
			return Config{}, errors.New("WEBHOOK_SIGNING_SECRET is required")
		}

		tolerance, err := time.ParseDuration(getenv("SIGNATURE_TOLERANCE", defaultSignatureTolerance.String()))
		if err != nil {
			return Config{}, fmt.Errorf("parse SIGNATURE_TOLERANCE: %w", err)
		}
		if tolerance <= 0 {
			return Config{}, fmt.Errorf("SIGNATURE_TOLERANCE must be positive, got %s", tolerance)
		}
		cfg.SignatureTolerance = tolerance

		// An unset or empty WORKER_QUEUE falls back to the default queue.
		cfg.WorkerQueue = getenv("WORKER_QUEUE", defaultWorkerQueue)

		demoMode, err := strconv.ParseBool(getenv("DEMO_MODE", "false"))
		if err != nil {
			return Config{}, fmt.Errorf("parse DEMO_MODE: %w", err)
		}
		cfg.DemoMode = demoMode

		loadgenIngest, err := strconv.ParseBool(getenv("LOADGEN_INGEST", "false"))
		if err != nil {
			return Config{}, fmt.Errorf("parse LOADGEN_INGEST: %w", err)
		}
		cfg.LoadgenIngest = loadgenIngest
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", defaultLogLevel))); err != nil {
		return Config{}, fmt.Errorf("parse LOG_LEVEL: %w", err)
	}

	switch cfg.LogFormat {
	case "json", "text":
	default:
		return Config{}, fmt.Errorf("LOG_FORMAT must be \"json\" or \"text\", got %q", cfg.LogFormat)
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
