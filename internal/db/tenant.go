// Package db — tenant-scope helpers used by all three driver backends.
package db

import (
	"context"
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TenantScope resolves, for collection under ctx, whether and which tenant must
// be enforced. Returns:
//   - inject=false                 → platform scope or global collection (no predicate)
//   - inject=true, tenantID=<slug> → stamp/predicate with that tenant
//   - err=ErrNoTenant              → scoped collection, naked context (fail-closed)
func TenantScope(ctx context.Context, collection string) (tenantID string, inject bool, err error) {
	if IsGlobalCollection(collection) || snoozetypes.IsPlatformScope(ctx) {
		return "", false, nil
	}
	t, ok := snoozetypes.TenantFrom(ctx)
	if !ok || t == "" {
		return "", false, fmt.Errorf("%w", snoozetypes.ErrNoTenant)
	}
	return t, true, nil
}

// WithTenantCond returns cond AND tenant_id=tenantID without double-wrapping.
// condition.And unwraps single-child conjunctions, so a bare user cond becomes a
// flat two-child AND, never a nested AND-of-AND.
func WithTenantCond(cond condition.Cond, tenantID string) condition.Cond {
	pred := condition.Equals("tenant_id", tenantID)
	if cond.IsZero() {
		return pred
	}
	return condition.And(cond, pred)
}
