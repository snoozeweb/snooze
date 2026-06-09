package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
)

func TestBootstrapDB_FirstBoot(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	require.NoError(t, BootstrapDB(context.Background(), drv, ""))

	roles := drv.docs(roleCollection)
	require.Len(t, roles, 3)

	names := make(map[string]bool, len(roles))
	for _, r := range roles {
		names[r["name"].(string)] = true
	}
	require.True(t, names["admin"])
	require.True(t, names["viewer"])
	require.True(t, names["notifications"])

	rules := drv.docs(aggregateRuleCollection)
	require.Len(t, rules, 1)

	general := drv.docs(generalCollection)
	require.Len(t, general, 1)
	require.Equal(t, true, general[0][bootstrapMarkerField])
}

func TestBootstrapDB_Idempotent(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	require.NoError(t, BootstrapDB(context.Background(), drv, ""))
	firstWriteCount := drv.writeCount(roleCollection)

	require.NoError(t, BootstrapDB(context.Background(), drv, ""))
	// Marker present → no further writes to roles.
	require.Equal(t, firstWriteCount, drv.writeCount(roleCollection))

	// Roles must remain at 3.
	require.Len(t, drv.docs(roleCollection), 3)
}

func TestBootstrapDB_NilDriver(t *testing.T) {
	t.Parallel()
	require.Error(t, BootstrapDB(context.Background(), nil, ""))
}

func TestBootstrapDB_MarkerVariants(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	// Seed an "init_db: false" marker — must NOT short-circuit.
	drv.seed(generalCollection, db.Document{bootstrapMarkerField: false})
	require.NoError(t, BootstrapDB(context.Background(), drv, ""))
	require.Len(t, drv.docs(roleCollection), 3)
}

func TestDefaultRoles_AdminGroupSeed(t *testing.T) {
	t.Parallel()

	adminRole := func(roles []db.Document) db.Document {
		for _, r := range roles {
			if r["name"] == "admin" {
				return r
			}
		}
		return nil
	}

	// OIDC enabled: the configured admin value seeds the admin role's groups.
	withGroup := adminRole(defaultRoles("Admin"))
	require.NotNil(t, withGroup)
	require.Equal(t, []string{"Admin"}, withGroup["groups"])

	// OIDC disabled (empty value): no groups, so a literal "Admin" LDAP/local
	// group cannot accidentally grant admin.
	noGroup := adminRole(defaultRoles(""))
	require.NotNil(t, noGroup)
	groups, ok := noGroup["groups"].([]string)
	require.True(t, ok, "groups should be present as []string")
	require.Empty(t, groups)
}
