package middleware

import (
	"net/http"
	"strings"
)

// CORSConfig drives the CORS middleware. Empty AllowOrigins disables CORS
// (the middleware becomes a passthrough). A single "*" entry allows every
// origin; anything else is matched exactly.
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORS is the canonical configuration for development: every origin,
// every standard verb, the headers we accept on auth and tracing, no
// credentials, and a 24h preflight cache.
func DefaultCORS() CORSConfig {
	return CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type", "X-Request-ID"},
		MaxAge:       86400,
	}
}

// CORS returns a chi-compatible CORS middleware. Replaces the third-party
// go-chi/cors dependency with a small first-party implementation to avoid a
// new external module for the API package.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	allowAll := contains(cfg.AllowOrigins, "*")
	allowedMethods := strings.Join(defaultIfEmpty(cfg.AllowMethods, []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}), ", ")
	allowedHeaders := strings.Join(defaultIfEmpty(cfg.AllowHeaders, []string{"Authorization", "Content-Type"}), ", ")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || len(cfg.AllowOrigins) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			if !allowAll && !contains(cfg.AllowOrigins, origin) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			if allowAll && !cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
			}
			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", allowedMethods)
				h.Set("Access-Control-Allow-Headers", allowedHeaders)
				if cfg.MaxAge > 0 {
					h.Set("Access-Control-Max-Age", itoa(cfg.MaxAge))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func defaultIfEmpty(v []string, fallback []string) []string {
	if len(v) == 0 {
		return fallback
	}
	return v
}

func itoa(n int) string {
	// avoid importing strconv twice for one call
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
