package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// SkipPredicate decides whether a request should bypass authentication.
// The middleware uses it to short-circuit before parsing the Authorization
// header — the canonical use case is /healthz, /readyz, /metrics, /login.
type SkipPredicate func(r *http.Request) bool

// APIKeyAuthenticator resolves a raw API key into claims. *auth.APIKeyStore
// satisfies it. Passed to Auth so a snz_-prefixed Bearer token authenticates
// as a user without a login JWT.
type APIKeyAuthenticator interface {
	Resolve(ctx context.Context, raw string) (snoozetypes.Claims, error)
}

// Auth returns a chi middleware that validates the Authorization: Bearer
// token, then stores the resulting Claims on the request context
// (auth.WithClaims) and stamps the tenant slug (auth.WithTenant). A token
// prefixed with auth.APIKeyPrefix is resolved via keys (when non-nil);
// otherwise it is verified as a login JWT via engine. A missing or invalid
// token yields a 401 ErrEnvelope. skip lets the caller bypass the check for
// public endpoints.
func Auth(engine *auth.TokenEngine, keys APIKeyAuthenticator, skip SkipPredicate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			if header == "" {
				writeUnauthorized(w, r, "missing authorization header")
				return
			}
			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeUnauthorized(w, r, "expected Authorization: Bearer <token>")
				return
			}
			token := parts[1]

			var claims snoozetypes.Claims
			if keys != nil && strings.HasPrefix(token, auth.APIKeyPrefix) {
				c, err := keys.Resolve(r.Context(), token)
				if err != nil {
					writeUnauthorized(w, r, "invalid api key")
					return
				}
				claims = c
			} else {
				if engine == nil {
					writeUnauthorized(w, r, "auth not configured")
					return
				}
				c, err := engine.Verify(token)
				if err != nil {
					writeUnauthorized(w, r, "invalid token")
					return
				}
				claims = c
			}

			// Stamp claims on context (existing behaviour).
			ctx := auth.WithClaims(r.Context(), claims)
			// Stamp tenant on context (D3). Empty claim (legacy token) falls
			// back to DefaultTenant.
			tenantID := claims.TenantID
			if tenantID == "" {
				tenantID = snoozetypes.DefaultTenant
			}
			ctx = auth.WithTenant(ctx, tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeUnauthorized writes a 401 ErrEnvelope directly (we do not import the
// parent api package to keep the import graph DAG-shaped).
func writeUnauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	envelope := snoozetypes.ErrEnvelope{
		Error: snoozetypes.ErrBody{
			Code:      "unauthorized",
			Message:   msg,
			RequestID: telemetry.RequestIDFrom(r.Context()),
			TraceID:   telemetry.TraceIDFrom(r.Context()),
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="snooze"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(envelope)
}
