// internal/migrate/migrate_test.go
package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestScopedCollections_DoesNotContainGlobals(t *testing.T) {
	t.Parallel()
	globals := map[string]struct{}{
		"tenant":  {},
		"secrets": {},
		"nodes":   {},
	}
	for _, c := range TenantScopedCollections {
		_, isGlobal := globals[c]
		require.False(t, isGlobal, "TenantScopedCollections must not contain global collection %q", c)
	}
}

func TestScopedCollections_ContainsExpected(t *testing.T) {
	t.Parallel()
	required := []string{"record", "rule", "user", "role", "snooze", "aggregaterule",
		"notification", "audit", "stats", "settings", "refresh_token", "heartbeat"}
	have := make(map[string]struct{}, len(TenantScopedCollections))
	for _, c := range TenantScopedCollections {
		have[c] = struct{}{}
	}
	for _, want := range required {
		_, ok := have[want]
		require.True(t, ok, "TenantScopedCollections must include %q", want)
	}
}

func TestIsAlreadyMigrated_False(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	pctx := auth.WithPlatformScope(context.Background())
	done, err := isAlreadyMigrated(pctx, drv)
	require.NoError(t, err)
	require.False(t, done)
}

func TestIsAlreadyMigrated_True(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed(migrationMarkerCollection, db.Document{migrationMarkerField: true})
	pctx := auth.WithPlatformScope(context.Background())
	done, err := isAlreadyMigrated(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)
}

func TestBackfillTenantID_StampsAllDocs(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("record", db.Document{"host": "h1"})
	drv.seed("rule", db.Document{"name": "r1"})
	drv.seed("user", db.Document{"name": "alice", "method": "local"})

	pctx := auth.WithPlatformScope(context.Background())
	n, err := backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	require.NoError(t, err)
	require.Equal(t, 3, n, "expected 3 total docs stamped")

	for _, col := range []string{"record", "rule", "user"} {
		for _, doc := range drv.docs(col) {
			require.Equal(t, snoozetypes.DefaultTenant, doc["tenant_id"],
				"collection %q doc missing tenant_id", col)
		}
	}
}

func TestBackfillTenantID_SkipsAlreadyStamped(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("record",
		db.Document{"host": "h1", "tenant_id": snoozetypes.DefaultTenant},
		db.Document{"host": "h2"},
	)
	pctx := auth.WithPlatformScope(context.Background())
	n, err := backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	require.NoError(t, err)
	// Only h2 lacked tenant_id.
	require.Equal(t, 1, n)
}

func TestBackfillTenantID_StampsHeartbeat(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("heartbeat", db.Document{"name": "nightly", "interval": float64(60)})

	pctx := auth.WithPlatformScope(context.Background())
	n, err := backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	require.NoError(t, err)
	require.Equal(t, 1, n, "the pre-existing heartbeat row must be stamped")

	for _, doc := range drv.docs("heartbeat") {
		require.Equal(t, snoozetypes.DefaultTenant, doc["tenant_id"],
			"heartbeat row must receive tenant_id=default")
	}
}

func TestBackfillTenantID_SkipsGlobalCollections(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("tenant", db.Document{"id": "acme"})
	drv.seed("secrets", db.Document{"key": "jwt"})
	pctx := auth.WithPlatformScope(context.Background())
	n, err := backfillTenantID(pctx, drv, snoozetypes.DefaultTenant)
	require.NoError(t, err)
	require.Equal(t, 0, n, "global collections must not be stamped")
	for _, doc := range drv.docs("tenant") {
		_, hasTenantID := doc["tenant_id"]
		require.False(t, hasTenantID, "global collection 'tenant' must not receive tenant_id")
	}
}

func TestEnsureDefaultTenant_CreatesTenantDoc(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	pctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, ensureDefaultTenant(pctx, drv))

	tenantDocs := drv.docs("tenant")
	require.Len(t, tenantDocs, 1)
	require.Equal(t, snoozetypes.DefaultTenant, tenantDocs[0]["id"])
	require.Equal(t, "active", tenantDocs[0]["status"])
}

func TestEnsureDefaultTenant_Idempotent(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("tenant", db.Document{
		"id":     snoozetypes.DefaultTenant,
		"status": "active",
	})
	pctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, ensureDefaultTenant(pctx, drv))

	tenantDocs := drv.docs("tenant")
	require.Len(t, tenantDocs, 1, "must not create duplicate tenant docs")
}

func TestEnsurePlatformAdminRole_CreatesRole(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	pctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, ensurePlatformAdminRole(pctx, drv))

	roles := drv.docs("role")
	var found db.Document
	for _, r := range roles {
		if r["name"] == auth.PlatformAdminRole {
			found = r
			break
		}
	}
	require.NotNil(t, found, "platform_admin role must be created")

	perms, _ := found["permissions"].([]any)
	permSet := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		permSet[p.(string)] = struct{}{}
	}
	require.Contains(t, permSet, auth.PermReadTenant)
	require.Contains(t, permSet, auth.PermWriteTenant)
}

func TestEnsurePlatformAdminRole_Idempotent(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("role", db.Document{
		"name":        auth.PlatformAdminRole,
		"permissions": []any{auth.PermReadTenant, auth.PermWriteTenant},
	})
	pctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, ensurePlatformAdminRole(pctx, drv))

	roles := drv.docs("role")
	count := 0
	for _, r := range roles {
		if r["name"] == auth.PlatformAdminRole {
			count++
		}
	}
	require.Equal(t, 1, count, "must not duplicate platform_admin role")
}

func TestGrantRootPlatformAdmin_AddsRole(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	// Seed a root user that already has a "roles" list.
	drv.seed("user", db.Document{
		"name":      auth.RootUsername,
		"method":    auth.LocalMethod,
		"tenant_id": snoozetypes.DefaultTenant,
		"roles":     []any{"admin"},
	})

	pctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, grantRootPlatformAdmin(pctx, drv))

	users := drv.docs("user")
	require.Len(t, users, 1)
	roles, _ := users[0]["roles"].([]any)
	roleSet := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleSet[r.(string)] = struct{}{}
	}
	require.Contains(t, roleSet, auth.PlatformAdminRole,
		"root user must have platform_admin role")
	require.Contains(t, roleSet, "admin", "root user must keep existing roles")
}

func TestGrantRootPlatformAdmin_Idempotent(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("user", db.Document{
		"name":      auth.RootUsername,
		"method":    auth.LocalMethod,
		"tenant_id": snoozetypes.DefaultTenant,
		"roles":     []any{"admin", auth.PlatformAdminRole},
	})

	pctx := auth.WithPlatformScope(context.Background())
	require.NoError(t, grantRootPlatformAdmin(pctx, drv))

	users := drv.docs("user")
	roles, _ := users[0]["roles"].([]any)
	count := 0
	for _, r := range roles {
		if r.(string) == auth.PlatformAdminRole {
			count++
		}
	}
	require.Equal(t, 1, count, "platform_admin must appear exactly once")
}

func TestGrantRootPlatformAdmin_NoRootUser_NoError(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	pctx := auth.WithPlatformScope(context.Background())
	// No root user seeded: must not error (pre-boot environments).
	require.NoError(t, grantRootPlatformAdmin(pctx, drv))
}

func TestRunMultitenancyMigration_FullRun(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("record", db.Document{"host": "h1"}, db.Document{"host": "h2"})
	drv.seed("rule", db.Document{"name": "r1"})
	drv.seed("user", db.Document{
		"name": auth.RootUsername, "method": auth.LocalMethod,
		"roles": []any{"admin"},
	})
	drv.seed("role", db.Document{"name": "admin", "permissions": []any{"rw_all"}})

	require.NoError(t, RunMultitenancyMigration(context.Background(), drv))

	// Sentinel must be set.
	pctx := auth.WithPlatformScope(context.Background())
	done, err := isAlreadyMigrated(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)

	// Default tenant doc must exist.
	tenantDocs := drv.docs("tenant")
	require.Len(t, tenantDocs, 1)

	// Records must have tenant_id.
	for _, doc := range drv.docs("record") {
		require.Equal(t, snoozetypes.DefaultTenant, doc["tenant_id"])
	}

	// Root must have platform_admin.
	userDocs := drv.docs("user")
	require.Len(t, userDocs, 1)
	roles, _ := userDocs[0]["roles"].([]any)
	roleSet := make(map[string]struct{})
	for _, r := range roles {
		roleSet[r.(string)] = struct{}{}
	}
	require.Contains(t, roleSet, auth.PlatformAdminRole)
}

func TestRunMultitenancyMigration_Idempotent(t *testing.T) {
	t.Parallel()
	drv := newFakeDriver()
	drv.seed("record", db.Document{"host": "h1"})
	drv.seed("user", db.Document{
		"name": auth.RootUsername, "method": auth.LocalMethod,
		"tenant_id": snoozetypes.DefaultTenant,
		"roles":     []any{"admin", auth.PlatformAdminRole},
	})

	require.NoError(t, RunMultitenancyMigration(context.Background(), drv))
	require.NoError(t, RunMultitenancyMigration(context.Background(), drv))

	pctx := auth.WithPlatformScope(context.Background())
	done, _ := isAlreadyMigrated(pctx, drv)
	require.True(t, done)

	tenantDocs := drv.docs("tenant")
	require.Len(t, tenantDocs, 1, "second run must not duplicate tenant doc")
}
