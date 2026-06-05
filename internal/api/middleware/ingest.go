package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TenantStatusChecker is the narrow interface the IngestTenant middleware uses
// to check whether a resolved tenant is active or suspended. The concrete
// implementation reads from the tenant collection via db.Driver under platform
// scope. Nil disables the check (tests and single-tenant deploys that don't
// wire the tenant plugin).
type TenantStatusChecker interface {
	TenantStatus(ctx context.Context, tenantID string) (string, error)
}

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

// IngestTenant returns a middleware that resolves the ingest tenant from the
// presented Bearer token (or query param `token`) using resolver, stamps the
// result onto the request context via auth.WithTenant, and optionally checks
// whether the tenant is suspended via checker.
//
// Resolution rules (D4):
//   - Known token → use the mapped tenant slug.
//   - Unknown or absent token → fall back to snoozetypes.DefaultTenant.
//
// Suspended-tenant rejection: when checker is non-nil and returns status
// "suspended" for the resolved tenant, the middleware returns 503 Service
// Unavailable before reaching the handler. Active tenants pass through.
// A checker error is treated as "active" (fail-open on checker errors so a
// flaky DB read does not block all ingest).
func IngestTenant(resolver *TenantResolver, checker TenantStatusChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ingestPresentedToken(r)
			tenantID, _ := resolver.Lookup(token)
			// tenantID is always non-empty: Lookup falls back to DefaultTenant.

			if checker != nil {
				status, err := checker.TenantStatus(r.Context(), tenantID)
				if err == nil && status == snoozetypes.TenantStatusSuspended {
					writeSuspended(w, r, tenantID)
					return
				}
			}

			ctx := auth.WithTenant(r.Context(), tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
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

// writeSuspended writes a 503 response for a suspended tenant.
func writeSuspended(w http.ResponseWriter, _ *http.Request, tenantID string) {
	_ = tenantID // used for logging by callers; kept as parameter for future audit
	envelope := snoozetypes.ErrEnvelope{
		Error: snoozetypes.ErrBody{
			Code:    "tenant_suspended",
			Message: "this tenant is suspended; ingest is disabled",
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = encodeJSON(w, envelope)
}

// encodeJSON writes v as JSON to w, ignoring encode errors (headers already sent).
func encodeJSON(w http.ResponseWriter, v any) error {
	return json.NewEncoder(w).Encode(v)
}
