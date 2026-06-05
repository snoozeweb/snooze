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
	"log/slog"

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

// globalMigrationCollections is the set of collections that are
// platform-global and must never receive a tenant_id stamp. Mirrors
// db.IsGlobalCollection but kept local so the migrate package can run
// without importing the driver's registry state (which may not be
// initialized in a standalone migration binary).
var globalMigrationCollections = map[string]struct{}{
	"tenant":    {},
	"secrets":   {},
	"nodes":     {},
	"heartbeat": {},
}

// isGlobalForMigration reports whether the named collection is platform-global
// and must be excluded from backfill.
func isGlobalForMigration(name string) bool {
	_, ok := globalMigrationCollections[name]
	return ok
}

// backfillTenantID iterates TenantScopedCollections that exist in the DB,
// finds every document without a tenant_id field, and stamps it with
// tenantID. It returns the count of documents actually modified.
// ctx must carry WithPlatformScope.
func backfillTenantID(ctx context.Context, drv db.Driver, tenantID string) (int, error) {
	existing, err := drv.ListCollections(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate: list collections: %w", err)
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, c := range existing {
		existingSet[c] = struct{}{}
	}

	total := 0
	for _, col := range TenantScopedCollections {
		if isGlobalForMigration(col) {
			continue
		}
		if _, ok := existingSet[col]; !ok {
			continue
		}
		docs, _, err := drv.Search(ctx, col, condition.Cond{}, db.Page{})
		if err != nil {
			return total, fmt.Errorf("migrate: search %s: %w", col, err)
		}
		var toUpdate []db.Document
		for _, d := range docs {
			if tid, ok := d["tenant_id"]; ok && tid != "" {
				continue
			}
			cp := make(db.Document, len(d))
			for k, v := range d {
				cp[k] = v
			}
			cp["tenant_id"] = tenantID
			toUpdate = append(toUpdate, cp)
		}
		if len(toUpdate) == 0 {
			continue
		}
		if _, err := drv.Write(ctx, col, toUpdate, db.WriteOptions{
			Primary:    []string{"uid"},
			UpdateTime: false,
		}); err != nil {
			return total, fmt.Errorf("migrate: write %s: %w", col, err)
		}
		n := len(toUpdate)
		slog.Info("migrate: backfilled collection", "collection", col, "count", n)
		total += n
	}
	return total, nil
}
