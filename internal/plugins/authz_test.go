package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// boolPtr is a tiny readability helper — *bool literals are awkward inline.
func boolPtr(b bool) *bool { return &b }

func TestIsAuthorized_NoLoginBypasses(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule"}
	ctx := AuthzContext{
		PluginName: "rule",
		Method:     "GET",
		Claims:     snoozetypes.Claims{},
		NoLogin:    true,
	}
	require.True(t, IsAuthorized(meta, ctx))
}

func TestIsAuthorized_RootMethodBypasses(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule"}
	ctx := AuthzContext{
		PluginName: "rule",
		Method:     "DELETE",
		Claims:     snoozetypes.Claims{Method: "root"},
	}
	require.True(t, IsAuthorized(meta, ctx))
}

func TestIsAuthorized_WildcardAllPermission(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule"}
	for _, perm := range []string{"rw_all", "ro_all"} {
		t.Run(perm, func(t *testing.T) {
			// rw_all wins everywhere. ro_all wins on reads only.
			claims := snoozetypes.Claims{Permissions: []string{perm}}
			require.True(t, IsAuthorized(meta, AuthzContext{
				PluginName: "rule", Method: "GET", Claims: claims,
			}))
			expectWrite := perm == "rw_all"
			require.Equal(t, expectWrite, IsAuthorized(meta, AuthzContext{
				PluginName: "rule", Method: "POST", Claims: claims,
			}))
		})
	}
}

func TestIsAuthorized_PerPluginPermissions(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule"}
	// ro_rule allows GET on rule but not on a different plugin.
	roRule := snoozetypes.Claims{Permissions: []string{"ro_rule"}}
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "rule", Method: "GET", Claims: roRule,
	}))
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "rule", Method: "POST", Claims: roRule,
	}))
	require.False(t, IsAuthorized(Metadata{Name: "comment"}, AuthzContext{
		PluginName: "comment", Method: "GET", Claims: roRule,
	}))
	// rw_rule allows reads + writes on rule.
	rwRule := snoozetypes.Claims{Permissions: []string{"rw_rule"}}
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "rule", Method: "GET", Claims: rwRule,
	}))
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "rule", Method: "PATCH", Claims: rwRule,
	}))
}

func TestIsAuthorized_AnyTokenGrantsToEveryone(t *testing.T) {
	t.Parallel()
	// `read: [any]` makes the route readable by every authenticated caller,
	// because the authorizer adds `any` to the user's permission set.
	meta := Metadata{
		Name: "user",
		RouteDefaults: Route{
			AuthorizationPolicy: &AuthorizationPolicy{Read: []string{"any"}},
		},
	}
	noPerms := snoozetypes.Claims{} // No permissions at all.
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "user", Method: "GET", Claims: noPerms,
	}))
	// But write still requires an explicit grant.
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "user", Method: "PATCH", Claims: noPerms,
	}))
}

func TestIsAuthorized_ProvidesStylePermission(t *testing.T) {
	t.Parallel()
	// Mirrors comment/metadata.yaml: write requires the `can_comment`
	// permission that the plugin advertises via Provides.
	meta := Metadata{
		Name: "comment",
		RouteDefaults: Route{
			AuthorizationPolicy: &AuthorizationPolicy{
				Read:  []string{"any"},
				Write: []string{"can_comment"},
			},
		},
	}
	withPerm := snoozetypes.Claims{Permissions: []string{"can_comment"}}
	noPerm := snoozetypes.Claims{}
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "comment", Method: "POST", Claims: withPerm,
	}))
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "comment", Method: "POST", Claims: noPerm,
	}))
	// Reads are open to all (any).
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "comment", Method: "GET", Claims: noPerm,
	}))
}

func TestIsAuthorized_RouteOverrideTakesPrecedence(t *testing.T) {
	t.Parallel()
	// RouteDefaults locks the plugin down; a per-route override loosens
	// it for the named sub-path only.
	meta := Metadata{
		Name: "user",
		RouteDefaults: Route{
			AuthorizationPolicy: &AuthorizationPolicy{
				Read:  []string{"rw_user"},
				Write: []string{"rw_user"},
			},
		},
		Routes: map[string]Route{
			"/user_self": {
				AuthorizationPolicy: &AuthorizationPolicy{
					Read: []string{"any"},
				},
			},
		},
	}
	noPerm := snoozetypes.Claims{}
	// Default subpath is locked.
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "user", Method: "GET", Claims: noPerm,
	}))
	// Sub-route opens reads.
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "user", Method: "GET", RoutePath: "/user_self", Claims: noPerm,
	}))
	// But writes on the override still require rw_user (inherited).
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "user", Method: "PUT", RoutePath: "/user_self", Claims: noPerm,
	}))
}

func TestResolveRoute_OverlaySemantics(t *testing.T) {
	t.Parallel()
	meta := Metadata{
		RouteDefaults: Route{
			ClassName:       "UserRoute",
			PrimaryKey:      []string{"name", "method"},
			DuplicatePolicy: "reject",
			Authentication:  boolPtr(true),
		},
		Routes: map[string]Route{
			"/user_self": {
				// only override the policy and a duplicate flag — the
				// authentication+primary fields should inherit.
				AuthorizationPolicy: &AuthorizationPolicy{Read: []string{"any"}},
				DuplicatePolicy:     "update",
			},
			"/anon": {
				Authentication: boolPtr(false),
			},
		},
	}

	// Pure defaults.
	d := meta.ResolveRoute("")
	require.Equal(t, "UserRoute", d.ClassName)
	require.Equal(t, []string{"name", "method"}, d.PrimaryKey)
	require.Equal(t, "reject", d.DuplicatePolicy)
	require.True(t, meta.AuthenticationRequired(""))

	// /user_self inherits ClassName + PrimaryKey from defaults, overrides
	// DuplicatePolicy + adds AuthorizationPolicy.
	r := meta.ResolveRoute("/user_self")
	require.Equal(t, "UserRoute", r.ClassName)
	require.Equal(t, []string{"name", "method"}, r.PrimaryKey)
	require.Equal(t, "update", r.DuplicatePolicy)
	require.NotNil(t, r.AuthorizationPolicy)
	require.Equal(t, []string{"any"}, r.AuthorizationPolicy.Read)
	require.True(t, meta.AuthenticationRequired("/user_self"))

	// /anon overrides Authentication.
	require.False(t, meta.AuthenticationRequired("/anon"))
}

func TestAuthenticationRequired_DefaultsToTrue(t *testing.T) {
	t.Parallel()
	// Plugin with no authentication field → still required.
	require.True(t, Metadata{}.AuthenticationRequired(""))
	// Explicit true.
	require.True(t, Metadata{
		RouteDefaults: Route{Authentication: boolPtr(true)},
	}.AuthenticationRequired(""))
	// Explicit false.
	require.False(t, Metadata{
		RouteDefaults: Route{Authentication: boolPtr(false)},
	}.AuthenticationRequired(""))
}
