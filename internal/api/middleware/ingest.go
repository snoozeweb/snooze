package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// IngestToken returns a middleware that gates inbound webhook requests on a
// shared secret. When token is empty the middleware is a no-op — webhook
// receivers stay unauthenticated, matching 1.5.0 and keeping existing
// deployments working. When token is set, every request must present it as
// either `Authorization: Bearer <token>` or a `?token=<token>` query
// parameter; otherwise it is rejected with 401 before reaching the receiver.
// The comparison is constant-time.
func IngestToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		want := []byte(token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(ingestPresentedToken(r))
			if subtle.ConstantTimeCompare(got, want) == 1 {
				next.ServeHTTP(w, r)
				return
			}
			writeUnauthorized(w, r, "invalid or missing ingest token")
		})
	}
}

// ingestPresentedToken extracts the caller's token from either the
// Authorization: Bearer header or the `token` query parameter.
func ingestPresentedToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if parts := strings.SplitN(h, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1]
		}
	}
	return r.URL.Query().Get("token")
}
