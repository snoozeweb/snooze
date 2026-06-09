package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestRoleResolver_Resolve_DirectRoles(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":   "alice",
		"method": LocalMethod,
		"roles":  []string{"reader"},
	})
	fdb.seed(RoleCollection,
		db.Document{"name": "reader", "permissions": []string{"read"}, "groups": []string{}},
		db.Document{"name": "writer", "permissions": []string{"write"}, "groups": []string{"writers"}},
	)

	r := NewRoleResolver(fdb)
	roles, perms, err := r.Resolve(context.Background(), Identity{Username: "alice", Method: LocalMethod})
	require.NoError(t, err)
	require.Equal(t, []string{"reader"}, roles)
	require.Equal(t, []string{"read"}, perms)
}

func TestRoleResolver_Resolve_GroupMappedRoles(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":   "bob",
		"method": "ldap",
		"groups": []string{"writers"},
	})
	fdb.seed(RoleCollection,
		db.Document{"name": "reader", "permissions": []string{"read"}, "groups": []string{"readers"}},
		db.Document{"name": "writer", "permissions": []string{"write", "read"}, "groups": []string{"writers"}},
	)
	r := NewRoleResolver(fdb)
	roles, perms, err := r.Resolve(context.Background(), Identity{
		Username: "bob",
		Method:   "ldap",
		Groups:   []string{"writers"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"writer"}, roles)
	require.ElementsMatch(t, []string{"write", "read"}, perms)
}

func TestRoleResolver_Resolve_UnionOfDirectAndGroup(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":         "carol",
		"method":       LocalMethod,
		"roles":        []string{"reader"},
		"static_roles": []string{"auditor"},
		"groups":       []string{"writers"},
	})
	fdb.seed(RoleCollection,
		db.Document{"name": "reader", "permissions": []string{"read"}, "groups": []string{}},
		db.Document{"name": "writer", "permissions": []string{"write"}, "groups": []string{"writers"}},
		db.Document{"name": "auditor", "permissions": []string{"audit"}, "groups": []string{}},
	)
	r := NewRoleResolver(fdb)
	roles, perms, err := r.Resolve(context.Background(), Identity{
		Username: "carol",
		Method:   LocalMethod,
		Groups:   []string{"writers"},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"reader", "writer", "auditor"}, roles)
	require.ElementsMatch(t, []string{"read", "write", "audit"}, perms)
}

func TestRoleResolver_Resolve_EmptyForUnknownUser(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	r := NewRoleResolver(fdb)
	roles, perms, err := r.Resolve(context.Background(), Identity{Username: "ghost", Method: "local"})
	require.NoError(t, err)
	require.Empty(t, roles)
	require.Empty(t, perms)
}

func TestHasPermission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims snoozetypes.Claims
		want   string
		ok     bool
	}{
		{"empty-want", snoozetypes.Claims{}, "", true},
		{"explicit-match", snoozetypes.Claims{Permissions: []string{"read"}}, "read", true},
		{"wildcard", snoozetypes.Claims{Permissions: []string{"rw_all"}}, "anything", true},
		{"miss", snoozetypes.Claims{Permissions: []string{"read"}}, "write", false},
		{"nil-perms", snoozetypes.Claims{}, "read", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.ok, HasPermission(tc.claims, tc.want))
		})
	}
}

func TestRoleResolver_Resolve_UsesContextTenant(t *testing.T) {
	t.Parallel()
	// The resolver must forward ctx to the DB calls; callers set WithTenant
	// before calling. This test verifies the contract: the same ctx passed in
	// is the one reaching the DB (fakeDB ignores it, but the test documents
	// the expected calling pattern for Phase 3).
	fdb := newFakeDB()
	fdb.seed(LocalCollection, db.Document{
		"name":   "dan",
		"method": LocalMethod,
		"roles":  []string{"viewer"},
	})
	fdb.seed(RoleCollection,
		db.Document{"name": "viewer", "permissions": []string{"ro_all"}, "groups": []string{}},
	)

	r := NewRoleResolver(fdb)
	// Set tenant on ctx — Phase 3 driver will enforce this; fakeDB ignores it.
	ctx := WithTenant(context.Background(), "acme")
	roles, perms, err := r.Resolve(ctx, Identity{
		Username: "dan",
		Method:   LocalMethod,
		TenantID: "acme",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles)
	require.Equal(t, []string{"ro_all"}, perms)
}

func TestRogueReservedRoles(t *testing.T) {
	ctx := context.Background()
	drv, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "s.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	pctx := snoozetypes.WithPlatformScope(ctx)
	_, err = drv.Write(pctx, RoleCollection, []db.Document{
		{"tenant_id": "default", "name": "platform_admin", "permissions": []any{"rw_tenant"}}, // legit, ignored
		{"tenant_id": "default", "name": "evil", "permissions": []any{"rw_tenant"}},            // rogue
		{"tenant_id": "default", "name": "ops", "permissions": []any{"rw_record"}},             // clean
	}, db.WriteOptions{Primary: []string{"tenant_id", "name"}})
	require.NoError(t, err)

	rogue, err := RogueReservedRoles(pctx, drv)
	require.NoError(t, err)
	require.Equal(t, []string{"default/evil"}, rogue)
}

func TestHasLiteralPermission(t *testing.T) {
	literal := snoozetypes.Claims{Permissions: []string{"ro_tenant", "rw_record"}}
	wildcard := snoozetypes.Claims{Permissions: []string{"rw_all"}}
	require.True(t, HasLiteralPermission(literal, "ro_tenant"))
	require.False(t, HasLiteralPermission(literal, "rw_tenant"))
	// The rw_all wildcard must NOT satisfy a literal platform-perm check.
	require.False(t, HasLiteralPermission(wildcard, "rw_tenant"))
	require.False(t, HasLiteralPermission(snoozetypes.Claims{}, "rw_tenant"))
}
