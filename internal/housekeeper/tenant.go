package housekeeper

import (
	"context"
	"fmt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// tenantCollection is the global registry of tenants.
const tenantCollection = "tenant"

// ForEachTenant lists active tenants (under platform scope) and invokes fn once
// per tenant with a context scoped to that tenant via auth.WithTenant. Suspended
// tenants are skipped. The list query runs under auth.WithPlatformScope so the
// driver does not attempt to inject a tenant_id predicate on the global tenant
// collection.
//
// Errors returned by fn abort iteration and are returned wrapped.
func ForEachTenant(ctx context.Context, d db.Driver, fn func(ctx context.Context, tenantID string) error) error {
	listCtx := auth.WithPlatformScope(ctx)
	docs, _, err := d.Search(listCtx, tenantCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("housekeeper: list tenants: %w", err)
	}
	for _, doc := range docs {
		status, _ := doc["status"].(string)
		if status == "suspended" {
			continue
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		tenantCtx := auth.WithTenant(ctx, id)
		if err := fn(tenantCtx, id); err != nil {
			return err
		}
	}
	return nil
}
