// Package middleware provides structured logging and panic recovery for HTTP
// handlers.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

type ctxKey struct{}

// LoggerFrom returns the request-scoped logger attached by Logging, falling
// back to the default logger when none is present.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

// Logging attaches a request-scoped logger carrying the chi correlation ID and
// emits one structured line per request with status, duration and bytes.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			correlationID := chimw.GetReqID(r.Context())
			reqLogger := logger.With(slog.String("correlation_id", correlationID))

			ctx := context.WithValue(r.Context(), ctxKey{}, reqLogger)
			recorder := &statusRecorder{ResponseWriter: w}

			defer func() {
				status := recorder.status
				if status == 0 {
					status = http.StatusOK
				}
				reqLogger.LogAttrs(ctx, slog.LevelInfo, "http request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Duration("duration", time.Since(start)),
					slog.Int("bytes", recorder.bytes),
					slog.String("remote_addr", r.RemoteAddr),
				)
			}()

			next.ServeHTTP(recorder, r.WithContext(ctx))
		})
	}
}

// Recoverer converts a panic into a logged 500 with a small JSON body, never
// leaking the internal error to the client.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				reqLogger := LoggerFrom(r.Context())
				reqLogger.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
				)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				if _, err := w.Write([]byte(`{"error":"internal server error"}` + "\n")); err != nil {
					reqLogger.ErrorContext(r.Context(), "write panic response", slog.Any("error", err))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	// Only the first WriteHeader counts: a handler calling WriteHeader twice
	// must not rewrite the logged status.
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
