package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
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

// TestBootstrap_AdminGroupMultiOIDC: with two OIDCProviders entries where only
// one (enabled) carries an AdminRoleValue, effectiveAdminGroup picks that value
// to seed the admin role's groups[].
func TestBootstrap_AdminGroupMultiOIDC(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OIDC = schema.OIDC{} // ensure the legacy scalar is not in play
	cfg.OIDCProviders = []schema.OIDC{
		{Method: "google", Provider: "google", Enabled: true}, // no AdminRoleValue
		{Method: "azure", Provider: "azure", Enabled: true, AdminRoleValue: "SnoozeAdmins"},
	}
	require.Equal(t, "SnoozeAdmins", effectiveAdminGroup(cfg))
}

// TestBootstrap_AdminGroupSkipsDisabled: a disabled entry's AdminRoleValue is
// not used even if it is the only one set.
func TestBootstrap_AdminGroupSkipsDisabled(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OIDC = schema.OIDC{}
	cfg.OIDCProviders = []schema.OIDC{
		{Method: "azure", Provider: "azure", Enabled: false, AdminRoleValue: "SnoozeAdmins"},
	}
	require.Equal(t, "", effectiveAdminGroup(cfg))
}

// TestBootstrap_AdminGroupLegacyFallback: with no OIDCProviders, the enabled
// legacy scalar OIDC's AdminRoleValue is used (backward compatibility).
func TestBootstrap_AdminGroupLegacyFallback(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OIDC.Enabled = true
	cfg.OIDC.AdminRoleValue = "Admin"
	require.Equal(t, "Admin", effectiveAdminGroup(cfg))
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
