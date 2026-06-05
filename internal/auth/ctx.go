package auth

import (
	"context"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ctxKey is the unexported key type for claims storage; the unexported nature
// prevents accidental collisions with other packages keying off context.Value.
type ctxKey struct{}

// WithClaims returns a derived context carrying c. The HTTP auth middleware
// is the canonical caller.
func WithClaims(ctx context.Context, c snoozetypes.Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// ClaimsFrom returns the claims previously attached by WithClaims. The bool
// is false when no claims are present (anonymous or pre-auth contexts).
func ClaimsFrom(ctx context.Context) (snoozetypes.Claims, bool) {
	v, ok := ctx.Value(ctxKey{}).(snoozetypes.Claims)
	return v, ok
}

// The tenant and platform-scope context helpers below are thin wrappers
// re-exporting the canonical implementations in pkg/snoozetypes/ctx.go. They
// live there (not here) to avoid the import cycle: internal/auth already
// imports internal/db, so internal/db cannot import internal/auth — but both
// may import pkg/snoozetypes. The context values are set and read via the
// snoozetypes keys, so crossing the package boundary is transparent.

// ErrNoTenant is the canonical fail-closed sentinel. Re-exported from
// snoozetypes so callers that import only internal/auth still find it here.
var ErrNoTenant = snoozetypes.ErrNoTenant

// WithTenant returns a derived context carrying the tenant slug.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return snoozetypes.WithTenant(ctx, tenantID)
}

// TenantFrom returns the tenant slug previously attached by WithTenant.
func TenantFrom(ctx context.Context) (string, bool) {
	return snoozetypes.TenantFrom(ctx)
}

// WithPlatformScope marks ctx as cross-tenant (escape hatch).
func WithPlatformScope(ctx context.Context) context.Context {
	return snoozetypes.WithPlatformScope(ctx)
}

// IsPlatformScope reports whether ctx was marked by WithPlatformScope.
func IsPlatformScope(ctx context.Context) bool {
	return snoozetypes.IsPlatformScope(ctx)
}
