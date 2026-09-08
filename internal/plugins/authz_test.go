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

func TestIsAuthorized_PlatformPermissions_GatesPlatformRoutes(t *testing.T) {
	t.Parallel()
	// A caller with rw_tenant must be authorized on a platform-tier route
	// (one that requires rw_tenant), but NOT on a normal plugin route.
	meta := Metadata{} // empty — no plugin name, no policy
	ctx := AuthzContext{
		PluginName: "",
		Method:     "POST",
		RoutePath:  "",
		Claims:     snoozetypes.Claims{Permissions: []string{"rw_tenant"}},
	}
	// Platform route: PluginName="" + policy explicitly lists rw_tenant.
	platformMeta := Metadata{}
	platformMeta.RouteDefaults.AuthorizationPolicy = &AuthorizationPolicy{
		Write: []string{"rw_tenant"},
		Read:  []string{"ro_tenant", "rw_tenant"},
	}
	require.True(t, IsAuthorized(platformMeta, ctx), "rw_tenant holder must pass a platform write route")

	// Normal plugin route: PluginName="rule", no special policy — rw_tenant alone must NOT grant access.
	ctx2 := AuthzContext{
		PluginName: "rule",
		Method:     "POST",
		RoutePath:  "",
		Claims:     snoozetypes.Claims{Permissions: []string{"rw_tenant"}},
	}
	require.False(t, IsAuthorized(meta, ctx2), "rw_tenant must not grant write on plugin routes")
}

func TestIsAuthorized_PlatformRead_AllowedWithRoTenant(t *testing.T) {
	t.Parallel()
	platformMeta := Metadata{}
	platformMeta.RouteDefaults.AuthorizationPolicy = &AuthorizationPolicy{
		Write: []string{"rw_tenant"},
		Read:  []string{"ro_tenant", "rw_tenant"},
	}
	ctx := AuthzContext{
		PluginName: "",
		Method:     "GET",
		RoutePath:  "",
		Claims:     snoozetypes.Claims{Permissions: []string{"ro_tenant"}},
	}
	require.True(t, IsAuthorized(platformMeta, ctx))
}

func TestIsAuthorized_PlatformWrite_PutPatchGatedByRwTenant(t *testing.T) {
	t.Parallel()
	// PUT/PATCH on a platform route (the tenant registry CRUD) are write
	// verbs, exactly like POST: they require rw_tenant. A read-only platform
	// caller (ro_tenant) must be rejected — otherwise it could mutate the
	// registry through PUT/PATCH even though it can only read it.
	platformMeta := Metadata{}
	platformMeta.RouteDefaults.AuthorizationPolicy = &AuthorizationPolicy{
		Write: []string{"rw_tenant"},
		Read:  []string{"ro_tenant", "rw_tenant"},
	}
	for _, method := range []string{"PUT", "PATCH"} {
		require.True(t, IsAuthorized(platformMeta, AuthzContext{
			PluginName: "",
			Method:     method,
			Claims:     snoozetypes.Claims{Permissions: []string{"rw_tenant"}},
		}), "%s with rw_tenant must be allowed on a platform write route", method)
		require.False(t, IsAuthorized(platformMeta, AuthzContext{
			PluginName: "",
			Method:     method,
			Claims:     snoozetypes.Claims{Permissions: []string{"ro_tenant"}},
		}), "%s with only ro_tenant must be denied on a platform write route", method)
	}
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

// TestIsRead_SearchPostCountsAsRead pins the generic search exception. Every
// plugin's POST /api/v1/<plugin>/search is mounted by the same
// mountCRUDWriteRoutes and runs the same searchHandler as GET ?q= — it is a
// list with the condition in the body. Classifying it as a write made
// `ro_<plugin>` a permission that can list but not search, which nobody would
// guess (the delivery log's Deliveries tab is the surface that exposed it).
func TestIsRead_SearchPostCountsAsRead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		plugin    string
		method    string
		path      string
		routePath string
		want      bool
	}{
		{"GET list", "notificationlog", "GET", "/api/v1/notificationlog", "", true},
		{"HEAD", "notificationlog", "HEAD", "/api/v1/notificationlog", "", true},
		{"POST canonical search", "notificationlog", "POST", "/api/v1/notificationlog/search", "", true},
		{"POST search trailing slash", "notificationlog", "POST", "/api/v1/notificationlog/search/", "", true},
		{"POST create", "notificationlog", "POST", "/api/v1/notificationlog", "", false},
		{"POST on a plugin whose NAME ends in search", "savedsearch", "POST", "/api/v1/savedsearch", "", false},
		{"POST search on that plugin", "savedsearch", "POST", "/api/v1/savedsearch/search", "", true},
		{"PUT", "notificationlog", "PUT", "/api/v1/notificationlog/abc", "", false},
		{"PATCH", "notificationlog", "PATCH", "/api/v1/notificationlog/abc", "", false},
		{"DELETE on the search path", "notificationlog", "DELETE", "/api/v1/notificationlog/search", "", false},
		{"POST with no path (non-HTTP caller)", "notificationlog", "POST", "", "", false},

		// N7: a RouteProvider plugin's own nested POST route is a WRITE, even
		// though it ends in /search. A suffix test handed it to ro_<plugin>.
		{"POST nested search", "foo", "POST", "/api/v1/foo/bar/search", "", false},
		{"POST search under another plugin", "foo", "POST", "/api/v1/bar/search", "", false},
		// A named sub-route (AuthorizeRoute, e.g. the webhook receiver mount)
		// is never the generic CRUD search, whatever it is spelled.
		{"POST named sub-route ending in search", "foo", "POST", "/api/v1/webhook/search", "/search", false},
		{"POST canonical path but named sub-route", "foo", "POST", "/api/v1/foo/search", "/search", false},
		// No plugin name means no canonical search path.
		{"POST search with no plugin name", "", "POST", "/api/v1/foo/search", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, AuthzContext{
				PluginName: tc.plugin,
				Method:     tc.method,
				Path:       tc.path,
				RoutePath:  tc.routePath,
			}.IsRead())
		})
	}
}

// TestIsAuthorized_SearchPostAllowedWithReadOnlyPermission is the end-to-end
// consequence: ro_<plugin> must satisfy POST /<plugin>/search, while POST on
// the collection root still requires rw_<plugin>.
func TestIsAuthorized_SearchPostAllowedWithReadOnlyPermission(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "notificationlog"}
	ro := snoozetypes.Claims{Permissions: []string{"ro_notificationlog"}}

	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "notificationlog", Method: "POST",
		Path: "/api/v1/notificationlog/search", Claims: ro,
	}), "a read-only role must be able to search")

	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "notificationlog", Method: "POST",
		Path: "/api/v1/notificationlog", Claims: ro,
	}), "creating must still require rw_")

	// A caller with no grant at all is still refused on search.
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "notificationlog", Method: "POST",
		Path: "/api/v1/notificationlog/search", Claims: snoozetypes.Claims{},
	}))

	// N7: a nested POST route that merely ENDS in /search is a write. A
	// RouteProvider plugin can mount one, and ro_<plugin> must not reach it.
	require.False(t, IsAuthorized(meta, AuthzContext{
		PluginName: "notificationlog", Method: "POST",
		Path: "/api/v1/notificationlog/bar/search", Claims: ro,
	}), "a nested /search route is a write, not the generic search")
	require.True(t, IsAuthorized(meta, AuthzContext{
		PluginName: "notificationlog", Method: "POST",
		Path:   "/api/v1/notificationlog/bar/search",
		Claims: snoozetypes.Claims{Permissions: []string{"rw_notificationlog"}},
	}), "rw_ still reaches it")
}
