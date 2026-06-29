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

// TestRoleResolver_Resolve_ServerManagedGroup exercises the server-managed
// group augmentation: a user with no `groups` on their user document gains a
// role because a "group" collection document lists them as a member and a role
// grants itself to that group's name. It also covers the negative case (a user
// not in the group gets nothing) and the union case (a server-managed group
// resolves alongside IdP-supplied groups).
func TestRoleResolver_Resolve_ServerManagedGroup(t *testing.T) {
	t.Parallel()
	fdb := newFakeDB()
	// carol has NO direct roles and NO groups on her user doc.
	fdb.seed(LocalCollection, db.Document{
		"name":   "carol",
		"method": LocalMethod,
	})
	// dave is a known user but not a member of the sre group.
	fdb.seed(LocalCollection, db.Document{
		"name":   "dave",
		"method": LocalMethod,
	})
	// Server-managed group "sre" lists carol (local) as a member. Members are
	// stored as an array of {username, method} objects — the post-JSON shape is
	// []any of map[string]any, which is what the driver returns.
	fdb.seed(GroupCollection, db.Document{
		"name": "sre",
		"members": []any{
			map[string]any{"username": "carol", "method": LocalMethod},
			map[string]any{"username": "bob", "method": "ldap"},
		},
	})
	fdb.seed(RoleCollection,
		db.Document{"name": "sre-oncall", "permissions": []string{"ack", "snooze"}, "groups": []string{"sre"}},
		db.Document{"name": "ops-role", "permissions": []string{"write"}, "groups": []string{"ops"}},
	)

	r := NewRoleResolver(fdb)

	// Positive: carol gains sre-oncall via her server-managed group membership.
	roles, perms, err := r.Resolve(context.Background(), Identity{Username: "carol", Method: LocalMethod})
	require.NoError(t, err)
	require.Equal(t, []string{"sre-oncall"}, roles)
	require.ElementsMatch(t, []string{"ack", "snooze"}, perms)

	// Negative: dave is not a member of any server-managed group and has no
	// direct roles / IdP groups, so resolves to nothing.
	roles, perms, err = r.Resolve(context.Background(), Identity{Username: "dave", Method: LocalMethod})
	require.NoError(t, err)
	require.Empty(t, roles)
	require.Empty(t, perms)

	// Union: carol carries an IdP-supplied group "ops" AND her server-managed
	// "sre" membership — both must resolve.
	roles, perms, err = r.Resolve(context.Background(), Identity{
		Username: "carol",
		Method:   LocalMethod,
		Groups:   []string{"ops"},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"sre-oncall", "ops-role"}, roles)
	require.ElementsMatch(t, []string{"ack", "snooze", "write"}, perms)
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
		{"tenant_id": "default", "name": "evil", "permissions": []any{"rw_tenant"}},           // rogue
		{"tenant_id": "default", "name": "ops", "permissions": []any{"rw_record"}},            // clean
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
