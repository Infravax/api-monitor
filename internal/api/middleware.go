package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/InfraVex/api-monitor/internal/id"
)

// middleware wraps an http.Handler with additional behavior.
type middleware func(http.Handler) http.Handler

// chain applies mw in order, so the first middleware listed is outermost
// (runs first on the way in, last on the way out).
func chain(h http.Handler, mw ...middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

type contextKey int

const requestIDContextKey contextKey = 0

// requestIDFromContext returns the request ID stored by RequestID, or ""
// if none is present.
func requestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDContextKey).(string)
	return v
}

// RequestID assigns a request ID to every request, exposing it both on the
// response (X-Request-ID header) and in the request context for
// downstream middleware/handlers to log alongside their own messages. It
// reuses internal/id (from the domain layer's ID generation) instead of
// adding a separate UUID dependency for this one purpose.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := id.New()
		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), requestIDContextKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code written by the handler so
// Logging can report it, since http.ResponseWriter does not expose it
// otherwise.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Logging logs one line per request: request ID, method, path, status, and
// duration. It wraps everything below it (including Recovery), so a
// recovered panic still gets a status/duration logged correctly.
func Logging(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.Info("http request",
				"request_id", requestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start),
			)
		})
	}
}

// Recovery recovers panics from the handler chain below it, logs them, and
// responds with a generic 500 instead of letting the panic take down the
// server. It must sit between Logging and the router so Logging still sees
// an accurate status code when a panic is recovered.
func Recovery(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						"request_id", requestIDFromContext(r.Context()),
						"panic", rec,
					)
					writeError(w, http.StatusInternalServerError, codeInternalError, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
