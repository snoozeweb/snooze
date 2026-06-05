// internal/migrate/sqlite_integration_test.go
package migrate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func newSQLiteDriver(t *testing.T) *sqlite.Driver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrate_test.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return drv
}

func TestRunMultitenancyMigration_SQLite_FullRun(t *testing.T) {
	drv := newSQLiteDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	// Seed pre-migration data (no tenant_id on any doc).
	_, err := drv.Write(pctx, "record",
		[]db.Document{{"host": "h1", "message": "test"}},
		db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	_, err = drv.Write(pctx, "user",
		[]db.Document{{"name": auth.RootUsername, "method": auth.LocalMethod, "roles": []any{"admin"}}},
		db.WriteOptions{Primary: []string{"name", "method"}, UpdateTime: false})
	require.NoError(t, err)

	_, err = drv.Write(pctx, "role",
		[]db.Document{{"name": "admin", "permissions": []any{"rw_all"}}},
		db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)

	// Run migration.
	require.NoError(t, RunMultitenancyMigration(ctx, drv))

	// Records gain tenant_id = default.
	records, _, err := drv.Search(pctx, "record", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, snoozetypes.DefaultTenant, records[0]["tenant_id"])

	// The tenant registry doc exists.
	tenants, _, err := drv.Search(pctx, "tenant", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, tenants, 1)
	require.Equal(t, snoozetypes.DefaultTenant, tenants[0]["id"])

	// The user gains tenant_id and the platform_admin role.
	users, _, err := drv.Search(pctx, "user", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, snoozetypes.DefaultTenant, users[0]["tenant_id"])
	roles, _ := users[0]["roles"].([]any)
	roleSet := make(map[string]struct{})
	for _, r := range roles {
		roleSet[r.(string)] = struct{}{}
	}
	require.Contains(t, roleSet, auth.PlatformAdminRole)

	// Second run is idempotent.
	require.NoError(t, RunMultitenancyMigration(ctx, drv))
	tenants2, _, err := drv.Search(pctx, "tenant", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, tenants2, 1, "idempotent: must not duplicate tenant doc")
}

func TestRunMultitenancyMigration_SQLite_EmptyDB(t *testing.T) {
	drv := newSQLiteDriver(t)
	require.NoError(t, RunMultitenancyMigration(context.Background(), drv))

	pctx := auth.WithPlatformScope(context.Background())
	done, err := isAlreadyMigrated(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)
}
