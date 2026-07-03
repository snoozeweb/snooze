package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config/schema"
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

// ProxyAuth provisions and resolves a trusted-header (auth-proxy) user from an
// Identity already parsed off the request headers. *auth.ProxyAuthenticator
// satisfies it. It is consulted only by AuthWithProxy and only when the
// auth-proxy mode is enabled.
type ProxyAuth interface {
	Authenticate(ctx context.Context, id auth.Identity, autoSignup bool) (snoozetypes.Claims, error)
}

// Auth returns a chi middleware that validates the Authorization: Bearer
// token, then stores the resulting Claims on the request context
// (auth.WithClaims) and stamps the tenant slug (auth.WithTenant). A token
// prefixed with auth.APIKeyPrefix is resolved via keys (when non-nil);
// otherwise it is verified as a login JWT via engine. A missing or invalid
// token yields a 401 ErrEnvelope. skip lets the caller bypass the check for
// public endpoints.
//
// Auth is the no-proxy form: it delegates to AuthWithProxy with a nil proxy and
// nil config, so the trusted-header branch is never entered and behaviour is
// byte-identical to the pre-auth-proxy middleware.
func Auth(engine *auth.TokenEngine, keys APIKeyAuthenticator, skip SkipPredicate) func(http.Handler) http.Handler {
	return AuthWithProxy(engine, keys, nil, nil, skip)
}

// AuthWithProxy is Auth plus an optional upstream-proxy (trusted-header) branch.
// When cfg is non-nil, cfg.Enabled is true, and proxy is non-nil, a request
// carrying the configured user header from a trusted IP authenticates via the
// proxy without a Bearer token. The proxy branch is a strict fall-through gate:
// a disabled mode, an untrusted client IP, or a missing user header all fall
// through to the normal Bearer/snz_/JWT path (they never 401 on the gate), so a
// valid Bearer JWT or snz_ key still authenticates when proxy mode is on. Only a
// successfully-authenticated proxy request returns early.
func AuthWithProxy(engine *auth.TokenEngine, keys APIKeyAuthenticator, proxy ProxyAuth, cfg *schema.AuthProxy, skip SkipPredicate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Trusted-header (auth-proxy) branch. Only entered when the mode is
			// explicitly enabled and a provisioner is wired. A failed gate
			// (untrusted IP / absent user header) deliberately falls through to
			// the Bearer path below — it never rejects, so non-browser callers
			// (CLI/syncer using snz_/JWT) keep working when proxy mode is on.
			if cfg != nil && cfg.Enabled && proxy != nil {
				if handled := tryProxyAuth(w, r, proxy, cfg, next); handled {
					return
				}
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

// tryProxyAuth runs the trusted-header gate and, on success, stamps claims +
// tenant and serves next. It returns handled=true when it has fully handled the
// request (either by serving next or by writing a 401/403 for a request that
// genuinely tried proxy auth). It returns handled=false to signal the caller to
// fall through to the Bearer/snz_/JWT path:
//
//   - the genuine TCP peer (PeerIP, NOT a forwarded header) is outside a
//     non-empty TrustedProxies list, or
//   - the configured user header is absent/blank.
//
// In both fall-through cases the request did NOT present proxy credentials, so
// a real Bearer/key request from the same caller must still be honored.
func tryProxyAuth(w http.ResponseWriter, r *http.Request, proxy ProxyAuth, cfg *schema.AuthProxy, next http.Handler) bool {
	// IP gate: when an allowlist is configured, the genuine TCP peer (PeerIP,
	// captured before chi RealIP and independent of any forwarding header) must
	// fall within it; otherwise this is not a trusted proxy request → fall
	// through (do not 401 here). Using PeerIP — not the XFF-aware ClientIP —
	// closes the X-Forwarded-For/X-Real-IP spoofing bypass.
	if len(cfg.TrustedProxies) > 0 && !ipInTrustedProxies(PeerIP(r), cfg.TrustedProxies) {
		return false
	}

	// Parse the identity from headers. A missing/blank user header is a
	// fall-through (lets the proxy itself reach /login etc.).
	id, ok := auth.IdentityFromProxyHeaders(r, *cfg)
	if !ok {
		return false
	}

	claims, err := proxy.Authenticate(r.Context(), id, cfg.AutoSignup)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotProvisioned) {
			writeForbidden(w, r)
			return true
		}
		writeUnauthorized(w, r, "proxy auth failed")
		return true
	}

	ctx := auth.WithClaims(r.Context(), claims)
	tenantID := claims.TenantID
	if tenantID == "" {
		tenantID = snoozetypes.DefaultTenant
	}
	ctx = auth.WithTenant(ctx, tenantID)
	next.ServeHTTP(w, r.WithContext(ctx))
	return true
}

// ipInTrustedProxies reports whether ip (a bare address, no port) falls within
// any entry of trusted, where each entry is an IP or a CIDR. Unparseable
// entries are skipped — validateAuthProxy rejects them at boot, so this is
// belt-and-braces.
func ipInTrustedProxies(ip string, trusted []string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, entry := range trusted {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			if network.Contains(parsed) {
				return true
			}
			continue
		}
		if single := net.ParseIP(entry); single != nil && single.Equal(parsed) {
			return true
		}
	}
	return false
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
