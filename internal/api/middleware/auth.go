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

// Bearer-resolution sentinels returned by resolveBearer. Each maps 1:1 to the
// 401 message AuthWithProxy wrote inline before the helper was extracted;
// keeping them distinct (rather than one generic error) lets AuthWithProxy
// reproduce those exact messages via err.Error() while OptionalAuth, which
// doesn't care about the reason, can treat every one identically.
var (
	errMissingAuthHeader = errors.New("missing authorization header")
	errMalformedBearer   = errors.New("expected Authorization: Bearer <token>")
	errInvalidAPIKey     = errors.New("invalid api key")
	errAuthNotConfigured = errors.New("auth not configured")
	errInvalidToken      = errors.New("invalid token")
)

// resolveBearer extracts and resolves the Authorization: Bearer token off r:
// a snz_-prefixed token is resolved via keys (when non-nil), anything else is
// verified as a login JWT via engine. It is the shared core of the strict
// AuthWithProxy path and the OptionalAuth path below. On any failure the
// returned error is one of the sentinels above.
func resolveBearer(r *http.Request, engine *auth.TokenEngine, keys APIKeyAuthenticator) (snoozetypes.Claims, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return snoozetypes.Claims{}, errMissingAuthHeader
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return snoozetypes.Claims{}, errMalformedBearer
	}
	token := parts[1]

	if keys != nil && strings.HasPrefix(token, auth.APIKeyPrefix) {
		c, err := keys.Resolve(r.Context(), token)
		if err != nil {
			return snoozetypes.Claims{}, errInvalidAPIKey
		}
		return c, nil
	}
	if engine == nil {
		return snoozetypes.Claims{}, errAuthNotConfigured
	}
	c, err := engine.Verify(token)
	if err != nil {
		return snoozetypes.Claims{}, errInvalidToken
	}
	return c, nil
}

// claimsToContext stamps claims on ctx (auth.WithClaims) and the resolved
// tenant slug (auth.WithTenant), falling back to snoozetypes.DefaultTenant
// when the claim's TenantID is empty (legacy token). Shared by the strict
// Bearer path, the proxy path, and OptionalAuth so the fallback rule can't
// drift between them.
func claimsToContext(ctx context.Context, claims snoozetypes.Claims) context.Context {
	ctx = auth.WithClaims(ctx, claims)
	tenantID := claims.TenantID
	if tenantID == "" {
		tenantID = snoozetypes.DefaultTenant
	}
	return auth.WithTenant(ctx, tenantID)
}

// OptionalAuth returns a chi middleware that stamps Claims/tenant on the
// context exactly like the strict Auth/AuthWithProxy path when the request
// carries a well-formed, resolvable Bearer token — but, unlike Auth, it never
// rejects the request. A missing Authorization header, a malformed Bearer
// value, or a token that fails to resolve (bad snz_ key / invalid or expired
// JWT) all serve the request unmodified: no claims, no tenant, no 401.
//
// This exists for public-but-tenant-aware endpoints — today, only
// GET /api/v1/config. That route is in skipAuth so the anonymous login screen
// can bootstrap branding before a token exists; the strict Auth middleware
// short-circuits skipAuth paths before ever parsing the Authorization header,
// so a logged-in caller hitting a skipAuth route never gets claims/tenant on
// the context either, even though the settings-plugin document the handler
// reads is tenant-scoped. Mounting OptionalAuth on that one route (in place of
// the global skip) lets the handler see the caller's tenant when a valid
// token is present, while an anonymous caller still gets served (with pure
// code defaults, since there's no tenant to overlay).
func OptionalAuth(engine *auth.TokenEngine, keys APIKeyAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := resolveBearer(r, engine, keys)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(claimsToContext(r.Context(), claims)))
		})
	}
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

			claims, err := resolveBearer(r, engine, keys)
			if err != nil {
				// err.Error() reproduces the exact 401 message this block wrote
				// inline before resolveBearer was extracted (see the sentinels
				// above resolveBearer's definition) — byte-identical behaviour.
				writeUnauthorized(w, r, err.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(claimsToContext(r.Context(), claims)))
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

	next.ServeHTTP(w, r.WithContext(claimsToContext(r.Context(), claims)))
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
