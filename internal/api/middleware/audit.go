package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/japannext/snooze/internal/telemetry"
)

// Audit returns a chi-compatible middleware that logs every served request on
// the supplied logger (typically loggers.Audit). The excludedPrefixes slice
// silences noisy paths (/metrics, /healthz, /readyz by default).
func Audit(logger *slog.Logger, excludedPrefixes []string) func(http.Handler) http.Handler {
	excl := append([]string{}, excludedPrefixes...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if logger == nil || isExcluded(r.URL.Path, excl) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			telemetry.WithRequest(r.Context(), logger).Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.String("remote", clientIP(r)),
				slog.Duration("duration", time.Since(start)),
				slog.Int("bytes", sw.bytes),
			)
		})
	}
}

func isExcluded(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// statusRecorder captures the response status code and byte count for the
// audit log without buffering the body.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.WriteHeader(http.StatusOK)
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// clientIP picks the client IP from X-Forwarded-For / X-Real-IP / RemoteAddr.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if r.RemoteAddr != "" {
		// trim port
		if i := strings.LastIndexByte(r.RemoteAddr, ':'); i > 0 {
			return r.RemoteAddr[:i]
		}
		return r.RemoteAddr
	}
	return ""
}
