// Package snoozetypes — context helpers for tenant and platform-scope plumbing.
// These live here (not in internal/auth) so that internal/db can import them
// without creating an import cycle (internal/auth already imports internal/db).
package snoozetypes

import (
	"context"
	"errors"
)

// ErrNoTenant is the canonical fail-closed sentinel returned by the driver
// layer when a tenant-scoped collection is accessed with neither a tenant nor
// platform scope in context. Driver backends wrap it with %w.
var ErrNoTenant = errors.New("auth: no tenant in context")

// tenantKey is the unexported context key for the tenant slug. The distinct
// named type prevents collisions with any other context value.
type tenantKey struct{}

// platformKey is the unexported context key for the platform-scope marker.
type platformKey struct{}

// WithTenant returns a derived context carrying the tenant slug. The HTTP auth
// middleware (authenticated requests) and the ingest middleware (unauthenticated
// ingress) are the canonical callers.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantFrom returns the tenant slug previously attached by WithTenant. The bool
// is false when no tenant is present.
func TenantFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tenantKey{}).(string)
	return v, ok
}

// WithPlatformScope marks ctx as cross-tenant. The driver skips tenant_id
// injection for contexts carrying this marker. Escape hatch for housekeeper,
// tenant-registry CRUD, admin socket, backup/restore, and migration ONLY.
func WithPlatformScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, platformKey{}, true)
}

// IsPlatformScope reports whether ctx was marked by WithPlatformScope.
func IsPlatformScope(ctx context.Context) bool {
	v, ok := ctx.Value(platformKey{}).(bool)
	return ok && v
}
