package httpapi

import (
	"net/http"
	"time"
)

// recoverMiddleware turns a panicking handler into a 500 response instead
// of taking the whole process down — one misbehaving request must never be
// able to drop every other tunnel's telemetry polling with it.
func (a *App) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.logger.Error("panic handling request", "path", r.URL.Path, "recover", rec)
				writeError(w, http.StatusInternalServerError, "Sunucu iç hatası.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware intentionally logs only method/path/status/latency —
// never the client's IP address, query string (which is where sessionId
// travels for GET /api/v1/telemetry), or request/response bodies. That is
// the no-logs policy from docs/SECURITY.md applied at the one place that
// sees every request.
func (a *App) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		a.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"durationMs", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
