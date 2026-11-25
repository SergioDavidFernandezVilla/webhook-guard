package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

var configKeys = []string{
	"HTTP_ADDR",
	"DATABASE_URL",
	"WEBHOOK_SIGNING_SECRET",
	"SIGNATURE_TOLERANCE",
	"WORKER_QUEUE",
	"LOG_LEVEL",
	"LOG_FORMAT",
}

// setEnv clears every configuration key and then applies the provided values,
// so tests never inherit the developer's real environment.
func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, key := range configKeys {
		t.Setenv(key, values[key])
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/db?sslmode=disable",
		"WEBHOOK_SIGNING_SECRET": "whsec_test",
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.SignatureTolerance != 5*time.Minute {
		t.Errorf("SignatureTolerance = %s, want %s", cfg.SignatureTolerance, 5*time.Minute)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %s, want %s", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
}

func TestLoad_CustomValues(t *testing.T) {
	env := validEnv()
	env["HTTP_ADDR"] = ":9090"
	env["SIGNATURE_TOLERANCE"] = "300s"
	env["LOG_LEVEL"] = "debug"
	env["LOG_FORMAT"] = "text"
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9090")
	}
	if cfg.SignatureTolerance != 300*time.Second {
		t.Errorf("SignatureTolerance = %s, want %s", cfg.SignatureTolerance, 300*time.Second)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %s, want %s", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "text")
	}
}

func TestLoadWithoutSecret_DoesNotRequireSigningSecret(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/db?sslmode=disable",
	})

	if _, err := LoadWithoutSecret(); err != nil {
		t.Fatalf("LoadWithoutSecret() error = %v, want nil", err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing secret error")
	}
}

func TestLoadWithoutSecret_IgnoresServiceOnlyVariables(t *testing.T) {
	// Subcommands must not fail on service-only variables they never use.
	setEnv(t, map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/db?sslmode=disable",
		"SIGNATURE_TOLERANCE": "not-a-duration",
		"WORKER_QUEUE":        "garbage queue value",
	})

	if _, err := LoadWithoutSecret(); err != nil {
		t.Fatalf("LoadWithoutSecret() error = %v, want nil", err)
	}
}

func TestLoad_WorkerQueueDefaultsAndOverrides(t *testing.T) {
	env := validEnv()
	setEnv(t, env)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WorkerQueue != "default" {
		t.Errorf("WorkerQueue = %q, want %q", cfg.WorkerQueue, "default")
	}

	setEnv(t, validEnv())
	t.Setenv("WORKER_QUEUE", "naive")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WorkerQueue != "naive" {
		t.Errorf("WorkerQueue = %q, want %q", cfg.WorkerQueue, "naive")
	}
}

func TestLoad_Errors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{
			name:    "missing database url",
			mutate:  func(env map[string]string) { delete(env, "DATABASE_URL") },
			wantErr: "DATABASE_URL is required",
		},
		{
			name:    "missing signing secret",
			mutate:  func(env map[string]string) { delete(env, "WEBHOOK_SIGNING_SECRET") },
			wantErr: "WEBHOOK_SIGNING_SECRET is required",
		},
		{
			name:    "invalid tolerance",
			mutate:  func(env map[string]string) { env["SIGNATURE_TOLERANCE"] = "not-a-duration" },
			wantErr: "parse SIGNATURE_TOLERANCE",
		},
		{
			name:    "non positive tolerance",
			mutate:  func(env map[string]string) { env["SIGNATURE_TOLERANCE"] = "-1s" },
			wantErr: "must be positive",
		},
		{
			name:    "invalid log level",
			mutate:  func(env map[string]string) { env["LOG_LEVEL"] = "verbose" },
			wantErr: "parse LOG_LEVEL",
		},
		{
			name:    "invalid log format",
			mutate:  func(env map[string]string) { env["LOG_FORMAT"] = "xml" },
			wantErr: "LOG_FORMAT must be",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			tc.mutate(env)
			setEnv(t, env)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
