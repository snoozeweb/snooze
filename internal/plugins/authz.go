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
	// than GET/HEAD is treated as a write.
	Method string
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

// IsRead reports whether ctx.Method is a "read" verb. GET and HEAD count as
// reads; everything else is treated as a write. Mirrors the
// `req.method in ['GET']` branch in 1.5.0's is_authorized.
func (c AuthzContext) IsRead() bool {
	switch strings.ToUpper(c.Method) {
	case http.MethodGet, http.MethodHead:
		return true
	}
	return false
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
