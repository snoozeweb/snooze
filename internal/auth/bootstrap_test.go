package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestEnsureRoot_FirstBoot(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	pwd, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)
	require.NotEmpty(t, pwd, "first boot must return a non-empty password")

	// The password must be base64url and >= 32 chars.
	require.GreaterOrEqual(t, len(pwd), 32)

	// The user document must exist with a verifiable bcrypt hash.
	doc, err := fdb.GetOne(context.Background(), LocalCollection, db.Document{
		"name":   RootUsername,
		"method": LocalMethod,
	})
	require.NoError(t, err)
	require.Equal(t, RootUsername, doc["name"])
	require.Equal(t, true, doc["enabled"])
	hash, _ := doc["password"].(string)
	require.NotEmpty(t, hash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)))
}

func TestEnsureRoot_AlreadyProvisioned(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	// Seed an existing root user.
	fdb.seed(LocalCollection, db.Document{
		"name":     RootUsername,
		"method":   LocalMethod,
		"enabled":  true,
		"password": "$2a$10$existinghashdoesnotmatter........................",
	})

	pwd, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)
	require.Empty(t, pwd, "must return empty password when root already exists")
}

func TestEnsureRoot_NilDriver(t *testing.T) {
	t.Parallel()
	_, err := EnsureRoot(context.Background(), nil)
	require.Error(t, err)
}

func TestEnsureRoot_AuthenticatesAfterBootstrap(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	pwd, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)

	// Round-trip: the generated password must authenticate via LocalProvider.
	p := NewLocalProvider(fdb)
	id, err := p.Authenticate(context.Background(), Credentials{Username: RootUsername, Password: pwd})
	require.NoError(t, err)
	require.Equal(t, RootUsername, id.Username)
	require.Equal(t, LocalMethod, id.Method)
}

func TestEnsureRoot_SeedsTenantID(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	_, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)

	doc, err := fdb.GetOne(context.Background(), LocalCollection, db.Document{
		"name":   RootUsername,
		"method": LocalMethod,
	})
	require.NoError(t, err)
	require.Equal(t, snoozetypes.DefaultTenant, doc["tenant_id"],
		"root user must be stamped with the default tenant")
}

func TestEnsureRoot_SeedsRootAsPlatformAdmin(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	_, err := EnsureRoot(context.Background(), fdb)
	require.NoError(t, err)

	doc, err := fdb.GetOne(context.Background(), LocalCollection, db.Document{
		"name":   RootUsername,
		"method": LocalMethod,
	})
	require.NoError(t, err)
	roles := stringSliceField(doc, "roles")
	require.Contains(t, roles, PlatformAdminRole,
		"root user must hold the platform_admin role")
}

func TestBootstrapDB_CreatesPlatformAdminRole(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	require.NoError(t, BootstrapDB(context.Background(), fdb))

	// platform_admin role must exist in the role collection.
	doc, err := fdb.GetOne(context.Background(), RoleCollection, db.Document{
		"name": PlatformAdminRole,
	})
	require.NoError(t, err)
	perms := stringSliceField(doc, "permissions")
	require.Contains(t, perms, PermReadTenant)
	require.Contains(t, perms, PermWriteTenant)
}

func TestBootstrapDB_CreatesDefaultTenant(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	require.NoError(t, BootstrapDB(context.Background(), fdb))

	// "default" tenant document must exist in the tenant collection.
	doc, err := fdb.GetOne(context.Background(), TenantCollection, db.Document{
		"id": snoozetypes.DefaultTenant,
	})
	require.NoError(t, err)
	require.Equal(t, snoozetypes.DefaultTenant, doc["id"])
	require.Equal(t, "active", doc["status"])
}

func TestBootstrapDB_Idempotent(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	require.NoError(t, BootstrapDB(context.Background(), fdb))
	// Second call must not error.
	require.NoError(t, BootstrapDB(context.Background(), fdb))

	// Still exactly one default tenant doc.
	docs := fdb.collections[TenantCollection]
	count := 0
	for _, d := range docs {
		if id, _ := d["id"].(string); id == snoozetypes.DefaultTenant {
			count++
		}
	}
	require.Equal(t, 1, count, "BootstrapDB must be idempotent for the default tenant")
}
