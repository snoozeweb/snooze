package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// TenantLookup resolves a per-tenant ingest token to the tenant it belongs to.
// The concrete implementation queries the global tenant collection via db.Driver.
type TenantLookup interface {
	// LookupByIngestToken returns the tenantID and status for the given token.
	// Returns ("", "", nil) when no tenant owns that token (fall through to default).
	LookupByIngestToken(ctx context.Context, token string) (tenantID, status string, err error)
}

// IngestTokenWithLookup extends the basic IngestToken middleware with per-tenant
// token lookup and suspension enforcement (D4). It:
//  1. Checks the global shared token (staticToken) — if it matches, falls through.
//  2. Calls lookup.LookupByIngestToken to resolve a per-tenant token.
//  3. If the resolved tenant is suspended, returns 403.
//  4. Stamps auth.WithTenant on the context with the resolved tenant (or DefaultTenant
//     when no/unknown token is found).
//
// When lookup is nil, behaviour degrades to the existing IngestToken logic with
// DefaultTenant stamped on every request.
func IngestTokenWithLookup(staticToken string, lookup TenantLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("X-Snooze-Token")
			ctx := r.Context()

			if token == "" {
				// No token → default tenant (D4 fallback).
				ctx = auth.WithTenant(ctx, snoozetypes.DefaultTenant)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Static global token takes priority.
			if staticToken != "" && token == staticToken {
				ctx = auth.WithTenant(ctx, snoozetypes.DefaultTenant)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if lookup == nil {
				ctx = auth.WithTenant(ctx, snoozetypes.DefaultTenant)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			tenantID, status, err := lookup.LookupByIngestToken(ctx, token)
			if err != nil || tenantID == "" {
				// Unknown token → default tenant (D4 fallback).
				ctx = auth.WithTenant(ctx, snoozetypes.DefaultTenant)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if status == "suspended" {
				writeForbidden(w, r)
				return
			}
			ctx = auth.WithTenant(ctx, tenantID)
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
