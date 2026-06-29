// Package auth implements the Snooze authentication and authorization layer:
// pluggable identity Providers (local, LDAP, anonymous), a HS256 JWT TokenEngine,
// RBAC resolution against the user/role collections, and a typed claims accessor
// for the HTTP middleware.
package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Errors surfaced by the auth package. Higher layers wrap these.
var (
	// ErrInvalidCredentials is returned when a username/password pair does not
	// match. Providers must return this (and only this) for bad credentials so
	// that callers cannot distinguish unknown user from wrong password.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUserDisabled is returned when the matching user exists but the account
	// is flagged as disabled.
	ErrUserDisabled = errors.New("user disabled")
	// ErrUnknownProvider is returned by Registry.Get when no provider has been
	// registered under the requested name.
	ErrUnknownProvider = errors.New("unknown auth provider")
	// ErrProviderDisabled is returned when an anonymous (or otherwise gated)
	// provider is queried but configuration has it turned off.
	ErrProviderDisabled = errors.New("auth provider disabled")
	// ErrRedirectProvider is returned by RedirectProvider.Authenticate to signal
	// that the provider uses the browser-redirect flow (Start/Callback), not a
	// password POST. The login routes special-case these providers.
	ErrRedirectProvider = errors.New("auth provider uses redirect flow")
	// ErrUserNotProvisioned is returned by ProxyAuthenticator.Authenticate when
	// an upstream-proxied user is unknown and auto-signup is disabled. The auth
	// middleware maps it to a 403 (matching Alerta's "user auto-signup is
	// disabled"); it is deliberately distinct from ErrInvalidCredentials.
	ErrUserNotProvisioned = errors.New("user not provisioned (auto-signup disabled)")
)

// Credentials carry the inputs to Provider.Authenticate. The Extra map is
// reserved for future SSO providers that need additional fields beyond a
// password (e.g. OIDC nonces).
type Credentials struct {
	Username string
	Password string
	Extra    map[string]string
}

// Identity is the canonical result of a successful authentication. Roles and
// permissions are resolved separately by RoleResolver.
type Identity struct {
	Username string
	Method   string
	TenantID string // tenant slug extracted from the login request's org field (D3/D10)
	Groups   []string
	// Email is the user's email address surfaced by the provider (OIDC "email"
	// claim, LDAP email attribute). It is transient — used only during the login
	// round-trip for attribute-based tenant resolution (Plan 29) — and is never
	// persisted to the user record.
	Email string
}

// Provider authenticates a set of credentials and produces an Identity. Name
// is the discriminator used by Registry.
type Provider interface {
	Name() string
	Authenticate(ctx context.Context, c Credentials) (Identity, error)
}

// EnableChecker is optionally implemented by providers whose visibility on the
// /api/v1/login backend index depends on runtime configuration. Providers that
// do not implement it are always listed.
type EnableChecker interface {
	IsEnabled(ctx context.Context) bool
}

// ProviderEnabled reports whether p should appear on the login backend list.
// Providers that don't implement EnableChecker are considered always-on.
func ProviderEnabled(ctx context.Context, p Provider) bool {
	if c, ok := p.(EnableChecker); ok {
		return c.IsEnabled(ctx)
	}
	return true
}

// RedirectProvider is implemented by browser-redirect (OIDC/OAuth) backends.
// They appear on the login index like password providers but are driven by the
// /api/v1/login/{name}/start and /callback routes instead of a password POST.
type RedirectProvider interface {
	Provider
	// DisplayName is the human label rendered on the login button.
	DisplayName() string
	// Icon is the icon key rendered on the login button.
	Icon() string
	// AuthCodeURL builds the IdP authorize URL carrying state, nonce and a PKCE
	// (S256) challenge derived from pkceVerifier.
	AuthCodeURL(ctx context.Context, state, nonce, pkceVerifier string) (string, error)
	// ExchangeAndVerify exchanges the authorization code, verifies the ID token
	// (signature/iss/aud/exp) and the nonce, and returns the resolved Identity.
	ExchangeAndVerify(ctx context.Context, code, nonce, pkceVerifier string) (Identity, error)
}

// SAMLProvider is implemented by the SAML2 SP-initiated backend. SAML does not
// fit RedirectProvider (whose AuthCodeURL/ExchangeAndVerify contract is
// OAuth-code-flow shaped): the IdP replies by POSTing a signed SAMLResponse to
// an ACS endpoint, not a GET with ?code=&state=. The login routes special-case
// these providers with a /start (redirect to IdP), /acs (POST consume) and
// /metadata (SP EntityDescriptor) trio. Like RedirectProvider, Authenticate
// returns ErrRedirectProvider.
type SAMLProvider interface {
	Provider
	// DisplayName is the human label rendered on the login button.
	DisplayName() string
	// Icon is the icon key rendered on the login button.
	Icon() string
	// AuthnRequestURL builds the SP-initiated HTTP-Redirect URL to the IdP SSO
	// endpoint, embedding relayState.
	AuthnRequestURL(ctx context.Context, relayState string) (string, error)
	// ParseAssertion validates the POSTed (base64) SAMLResponse — signature,
	// audience, conditions, NotOnOrAfter — and returns the resolved Identity.
	ParseAssertion(ctx context.Context, samlResponse string) (Identity, error)
	// Metadata returns the SP EntityDescriptor XML for the IdP to register.
	Metadata(ctx context.Context) ([]byte, error)
}

// Registry is a name-indexed collection of Providers. It is safe for
// concurrent use after construction.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register stores p under its Name(). A second Register call with the same name
// silently replaces the previous entry — providers are expected to be wired at
// boot time, before the HTTP server starts.
func (r *Registry) Register(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns the provider registered under name or ErrUnknownProvider.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
	return p, nil
}

// Names returns the registered provider names in a stable order. Used by the
// /login route to list backends.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for name := range r.providers {
		out = append(out, name)
	}
	return out
}
