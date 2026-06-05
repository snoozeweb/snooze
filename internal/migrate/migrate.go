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
	"errors"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// rewriteUserRolePKs stamps tenant_id on every user and role document so their
// compound PK [tenant_id, name, method] (user) or [tenant_id, name] (role)
// is complete. After Phase 2, new writes already carry tenant_id; this is the
// one-shot backfill for pre-migration rows.
// ctx must carry WithPlatformScope.
func rewriteUserRolePKs(ctx context.Context, drv db.Driver, tenantID string) error {
	type collPK struct {
		col     string
		primary []string
	}
	targets := []collPK{
		{"user", []string{"tenant_id", "name", "method"}},
		{"role", []string{"tenant_id", "name"}},
	}

	existing, err := drv.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("migrate: rewrite PKs: list collections: %w", err)
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, c := range existing {
		existingSet[c] = struct{}{}
	}

	for _, target := range targets {
		if _, ok := existingSet[target.col]; !ok {
			continue
		}
		docs, _, err := drv.Search(ctx, target.col, condition.Cond{}, db.Page{})
		if err != nil {
			return fmt.Errorf("migrate: rewrite PKs: search %s: %w", target.col, err)
		}
		if len(docs) == 0 {
			continue
		}
		toWrite := make([]db.Document, 0, len(docs))
		for _, d := range docs {
			cp := make(db.Document, len(d))
			for k, v := range d {
				cp[k] = v
			}
			cp["tenant_id"] = tenantID
			toWrite = append(toWrite, cp)
		}
		if _, err := drv.Write(ctx, target.col, toWrite, db.WriteOptions{
			Primary:    target.primary,
			UpdateTime: false,
		}); err != nil {
			return fmt.Errorf("migrate: rewrite PKs: write %s: %w", target.col, err)
		}
		slog.Info("migrate: rewrote PKs", "collection", target.col, "count", len(toWrite))
	}
	return nil
}

// ensureDefaultTenant upserts the reserved "default" tenant doc into the
// global tenant registry collection. Idempotent: upsert on PK ["id"].
// ctx must carry WithPlatformScope (tenant is a global collection, but we
// pass platform scope for consistency with the rest of the migration path).
func ensureDefaultTenant(ctx context.Context, drv db.Driver) error {
	_, err := drv.Write(ctx, "tenant", []db.Document{
		{
			"id":           snoozetypes.DefaultTenant,
			"display_name": "Default",
			"status":       "active",
		},
	}, db.WriteOptions{
		Primary:    []string{"id"},
		UpdateTime: true,
	})
	if err != nil {
		return fmt.Errorf("migrate: ensure default tenant: %w", err)
	}
	slog.Info("migrate: default tenant ensured")
	return nil
}

// ensurePlatformAdminRole upserts the platform_admin role with
// rw_tenant + ro_tenant permissions into the (global-under-platform-scope)
// role collection. The role is NOT tenant-scoped; it carries no tenant_id.
// Idempotent: upsert on PK ["name"].
// ctx must carry WithPlatformScope.
func ensurePlatformAdminRole(ctx context.Context, drv db.Driver) error {
	_, err := drv.Write(ctx, "role", []db.Document{
		{
			"name":        auth.PlatformAdminRole,
			"permissions": []any{auth.PermReadTenant, auth.PermWriteTenant},
		},
	}, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	if err != nil {
		return fmt.Errorf("migrate: ensure platform_admin role: %w", err)
	}
	slog.Info("migrate: platform_admin role ensured")
	return nil
}

// grantRootPlatformAdmin adds the platform_admin role to the root user in the
// default tenant if the user exists and does not already hold the role.
// A missing root user is not an error (e.g. a clean-slate migration before
// first boot). ctx must carry WithPlatformScope.
func grantRootPlatformAdmin(ctx context.Context, drv db.Driver) error {
	root, err := drv.GetOne(ctx, "user", db.Document{
		"name":      auth.RootUsername,
		"method":    auth.LocalMethod,
		"tenant_id": snoozetypes.DefaultTenant,
	})
	if err != nil {
		// Not found is not fatal; log and skip.
		slog.Info("migrate: root user not found, skipping platform_admin grant")
		return nil
	}

	// Build deduplicated roles list.
	existingRoles, _ := root["roles"].([]any)
	roleSet := make(map[string]struct{}, len(existingRoles)+1)
	for _, r := range existingRoles {
		if s, ok := r.(string); ok {
			roleSet[s] = struct{}{}
		}
	}
	if _, already := roleSet[auth.PlatformAdminRole]; already {
		slog.Info("migrate: root already has platform_admin, nothing to do")
		return nil
	}
	roleSet[auth.PlatformAdminRole] = struct{}{}

	newRoles := make([]any, 0, len(roleSet))
	for r := range roleSet {
		newRoles = append(newRoles, r)
	}

	cp := make(db.Document, len(root))
	for k, v := range root {
		cp[k] = v
	}
	cp["roles"] = newRoles

	if _, err := drv.Write(ctx, "user", []db.Document{cp}, db.WriteOptions{
		Primary:    []string{"tenant_id", "name", "method"},
		UpdateTime: false,
	}); err != nil {
		return fmt.Errorf("migrate: grant platform_admin: %w", err)
	}
	slog.Info("migrate: granted platform_admin to root")
	return nil
}

// RunMultitenancyMigration is the public entry point. It runs the full
// one-shot migration under platform scope and writes the completion sentinel.
// Safe to call multiple times; subsequent calls return immediately when the
// sentinel is already present.
//
// Steps:
//  1. Check sentinel — return if already done.
//  2. Ensure the "default" tenant doc in the tenant registry.
//  3. Ensure the platform_admin role.
//  4. Backfill tenant_id = "default" on all tenant-scoped collections.
//  5. Rewrite user and role PKs to include tenant_id.
//  6. Grant root the platform_admin role.
//  7. Write the completion sentinel.
func RunMultitenancyMigration(ctx context.Context, drv db.Driver) error {
	if drv == nil {
		return errors.New("migrate: nil db driver")
	}
	// All operations run under platform scope so the driver's
	// fail-closed tenant guard is bypassed (pre-migration docs have no
	// tenant_id yet).
	pctx := auth.WithPlatformScope(ctx)

	done, err := isAlreadyMigrated(pctx, drv)
	if err != nil {
		return err
	}
	if done {
		slog.Info("migrate: multitenancy migration already complete, skipping")
		return nil
	}

	slog.Info("migrate: starting multitenancy migration")

	if err := ensureDefaultTenant(pctx, drv); err != nil {
		return err
	}
	if err := ensurePlatformAdminRole(pctx, drv); err != nil {
		return err
	}
	n, err := backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	if err != nil {
		return err
	}
	slog.Info("migrate: backfill complete", "total_docs_stamped", n)

	if err := rewriteUserRolePKs(pctx, drv, snoozetypes.DefaultTenant); err != nil {
		return err
	}
	if err := grantRootPlatformAdmin(pctx, drv); err != nil {
		return err
	}
	if err := writeSentinel(pctx, drv); err != nil {
		return err
	}
	slog.Info("migrate: multitenancy migration complete")
	return nil
}
