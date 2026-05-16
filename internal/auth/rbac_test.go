package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
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
