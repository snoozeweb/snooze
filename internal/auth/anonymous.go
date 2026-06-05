package auth

import (
	"context"
	"fmt"
)

// AnonymousMethod is the auth method string set on Identity for anonymous logins.
const AnonymousMethod = "anonymous"

// AnonymousUsername is the canonical Identity.Username for anonymous sessions.
const AnonymousUsername = "anonymous"

// AnonymousProvider unconditionally succeeds when Enabled is true. The gate is
// driven by the general config's anonymous_enabled flag.
type AnonymousProvider struct {
	// Enabled mirrors schema.General.AnonymousEnabled at construction time.
	// Tests and tools can flip it without rebuilding a registry.
	Enabled bool
}

// NewAnonymousProvider returns an anonymous provider with the given enabled flag.
func NewAnonymousProvider(enabled bool) *AnonymousProvider {
	return &AnonymousProvider{Enabled: enabled}
}

// Name returns "anonymous".
func (a *AnonymousProvider) Name() string { return AnonymousMethod }

// IsEnabled reports whether the provider should appear on the login backend
// list. Implements auth.EnableChecker.
func (a *AnonymousProvider) IsEnabled(_ context.Context) bool { return a.Enabled }

// Authenticate ignores Credentials. Returns ErrProviderDisabled if the
// anonymous backend has been turned off.
func (a *AnonymousProvider) Authenticate(ctx context.Context, _ Credentials) (Identity, error) {
	if !a.Enabled {
		return Identity{}, fmt.Errorf("anonymous: %w", ErrProviderDisabled)
	}
	tenantID, _ := TenantFrom(ctx)
	return Identity{
		Username: AnonymousUsername,
		Method:   AnonymousMethod,
		TenantID: tenantID,
		Groups:   nil,
	}, nil
}
