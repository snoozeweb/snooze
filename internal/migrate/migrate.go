// Package migrate provides the one-shot multitenancy migration that backfills
// tenant_id = "default" on every existing document in tenant-scoped
// collections, rewrites user/role PKs to include the default tenant, creates
// the default tenant registry doc, and grants the root user the platform_admin
// role.
//
// The migration is idempotent: running it twice produces the same state as
// running it once. A sentinel document in the "general" collection marks
// completion.
//
// All operations run under auth.WithPlatformScope so the driver's tenant
// injection is bypassed (the collections are being bootstrapped for the first
// time and have no tenant_id yet).
package migrate

import (
	"context"
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// migrationMarkerCollection is where the idempotency sentinel lives.
const migrationMarkerCollection = "general"

// migrationMarkerField is the key whose presence (true) signals completion.
const migrationMarkerField = "multitenancy_v1"

// TenantScopedCollections is the complete, canonical list of collections that
// must receive tenant_id = DefaultTenant during migration. Global collections
// (tenant, secrets, nodes, heartbeat) are excluded; they never carry
// tenant_id.
//
// Keep this list in sync with §2 / §4 of the Shared Contract whenever a new
// plugin adds a collection.
var TenantScopedCollections = []string{
	"record",
	"rule",
	"aggregaterule",
	"snooze",
	"notification",
	"action",
	"user",
	"role",
	"refresh_token",
	"audit",
	"stats",
	"settings",
	"comment",
	"environment",
	"kv",
	"profile",
	"widget",
	"aggregate",
	"general",
}

// isAlreadyMigrated returns true when the migration sentinel is present. ctx
// must carry WithPlatformScope so the driver's global-or-platform bypass is
// active (general is in TenantScopedCollections; we read it under platform
// scope to avoid the fail-closed guard before migration has set any tenant_id).
func isAlreadyMigrated(ctx context.Context, drv db.Driver) (bool, error) {
	docs, _, err := drv.Search(ctx, migrationMarkerCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return false, fmt.Errorf("migrate: check sentinel: %w", err)
	}
	for _, d := range docs {
		if v, ok := d[migrationMarkerField]; ok {
			if b, _ := v.(bool); b {
				return true, nil
			}
		}
	}
	return false, nil
}

// writeSentinel stamps the migration-complete marker. It upserts (no primary
// given, so uid-based) so re-runs don't accumulate duplicate docs.
func writeSentinel(ctx context.Context, drv db.Driver) error {
	_, err := drv.Write(ctx, migrationMarkerCollection, []db.Document{
		{migrationMarkerField: true},
	}, db.WriteOptions{UpdateTime: true})
	if err != nil {
		return fmt.Errorf("migrate: write sentinel: %w", err)
	}
	return nil
}
