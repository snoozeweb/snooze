// Authorization (RBAC) for plugin CRUD routes. Ports the
// snooze/utils/functions.py::is_authorized + @authorize machinery from
// Snooze 1.5.0:
//
//   - Each plugin has implicit read/write permissions named `ro_<plugin>` and
//     `rw_<plugin>`. The catch-alls `ro_all` / `rw_all` grant access across
//     every plugin.
//   - A plugin's metadata.yaml may declare an authorization_policy with
//     additional `read` / `write` permission names — including the special
//     token `any`, which matches every authenticated caller because the
//     authorizer implicitly adds `any` to the user's permission set.
//   - Read methods (GET, HEAD) are satisfied by ANY read OR write permission
//     in the union (someone with write access can also read).
//   - Write methods (POST, PUT, PATCH, DELETE) require a write permission.
//
// The `root` JWT method (issued by the admin unix socket) and a server-wide
// `no_login` flag both short-circuit the check.

package plugins

import (
	"net/http"
	"strings"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// AuthzContext is the small bundle the authorizer needs about a request.
// It is decoupled from net/http so the same helper can be unit-tested
// without a server harness.
type AuthzContext struct {
	// PluginName is the registry name of the plugin owning the route
	// (Plugin.Name()). Used to derive the implicit `ro_<plugin>` /
	// `rw_<plugin>` grants.
	PluginName string
	// Method is the HTTP verb of the inbound request. Anything other
	// than GET/HEAD (and the POST .../search read, see IsRead) is treated
	// as a write.
	Method string
	// Path is the inbound request's URL path. Only consulted to recognise
	// the body-carrying search endpoint; leave empty when irrelevant.
	Path string
	// RoutePath is the path-key under Metadata.Routes; pass "" to
	// resolve only RouteDefaults.
	RoutePath string
	// Claims carries the verified JWT claims of the caller. The `root`
	// method bypasses every check.
	Claims snoozetypes.Claims
	// NoLogin mirrors core.no_login: when true the authorizer is a no-op
	// and every request is allowed through.
	NoLogin bool
}

// IsRead reports whether the request is a "read". GET and HEAD count, as does
// POST on the `/search` sub-path.
//
// The search exception is not a special case for one plugin: every plugin's
// POST /api/v1/<plugin>/search is mounted by the same generic
// mountCRUDWriteRoutes and runs the same searchHandler as GET ?q= — it is a
// list with the condition in the body instead of the query string (bodies
// avoid URL-length limits and base64-encoding the DSL). Counting it as a write
// meant a read-only role could list a collection but not search it, which
// nobody would guess from `ro_<plugin>`.
//
// Only the plugin's OWN canonical search path qualifies — see isSearchPath.
func (c AuthzContext) IsRead() bool {
	switch strings.ToUpper(c.Method) {
	case http.MethodGet, http.MethodHead:
		return true
	case http.MethodPost:
		return c.isSearchPath()
	}
	return false
}

// authzAPIPrefix is the mount prefix every plugin route lives under. It is a
// literal in the mount too (crud.go builds each subrouter as
// "/api/v1/"+p.Name(), and nothing chi.Mounts the API under a further prefix),
// so isSearchPath can compare the whole path instead of a suffix. If the prefix
// ever becomes configurable, this is the single place that has to learn about
// it — and the comparison must then drop to the "/"+PluginName+"/search"
// suffix.
const authzAPIPrefix = "/api/v1/"

// isSearchPath reports whether the request addresses the plugin's OWN generic
// search endpoint: the POST /api/v1/<plugin>/search that mountCRUDWriteRoutes
// installs, and nothing else. Trailing slashes are tolerated; query strings
// never reach this field (it is r.URL.Path).
//
// The match is exact rather than a "/search" suffix test, and RoutePath must be
// empty, because both of those loopholes downgraded real writes to reads:
//
//   - a RouteProvider plugin is free to mount its own nested POST route
//     (e.g. /api/v1/foo/bar/search), which a suffix test would hand to any
//     holder of ro_foo;
//   - a non-empty RoutePath means the caller is authorizing a NAMED sub-route
//     (AuthorizeRoute, used by the webhook receiver mount), never the generic
//     CRUD search — so a receiver whose WebhookPath ended in /search became
//     ingestion that a read-only token could drive.
//
// An empty PluginName has no canonical search path, so it never matches: a
// non-HTTP caller that only fills in Method is a write.
func (c AuthzContext) isSearchPath() bool {
	if c.RoutePath != "" || c.PluginName == "" {
		return false
	}
	return strings.TrimSuffix(c.Path, "/") == authzAPIPrefix+c.PluginName+"/search"
}

// IsAuthorized returns true when ctx.Claims is allowed to perform the
// requested method against the plugin route. The semantics mirror 1.5.0's
// is_authorized in snooze/utils/functions.py:141.
func IsAuthorized(meta Metadata, ctx AuthzContext) bool {
	if ctx.NoLogin {
		return true
	}
	if strings.EqualFold(ctx.Claims.Method, "root") {
		return true
	}

	rt := meta.ResolveRoute(ctx.RoutePath)
	plugin := ctx.PluginName

	read := map[string]struct{}{"ro_all": {}, "rw_all": {}}
	write := map[string]struct{}{"rw_all": {}}
	if plugin != "" {
		read["ro_"+plugin] = struct{}{}
		read["rw_"+plugin] = struct{}{}
		write["rw_"+plugin] = struct{}{}
	}
	if rt.AuthorizationPolicy != nil {
		for _, p := range rt.AuthorizationPolicy.Read {
			if p != "" {
				read[p] = struct{}{}
			}
		}
		for _, p := range rt.AuthorizationPolicy.Write {
			if p != "" {
				write[p] = struct{}{}
			}
		}
	}

	// `any` is implicitly part of every authenticated user's permission
	// set — mirrors the 1.5.0 line `auth_permissions = permissions | {'any'}`.
	have := map[string]struct{}{"any": {}}
	for _, p := range ctx.Claims.Permissions {
		if p != "" {
			have[p] = struct{}{}
		}
	}

	var valid map[string]struct{}
	if ctx.IsRead() {
		// Reads accept either a read or a write permission.
		valid = make(map[string]struct{}, len(read)+len(write))
		for k := range read {
			valid[k] = struct{}{}
		}
		for k := range write {
			valid[k] = struct{}{}
		}
	} else {
		valid = write
	}

	for p := range have {
		if _, ok := valid[p]; ok {
			return true
		}
	}
	return false
}
