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
