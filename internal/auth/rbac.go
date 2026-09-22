package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// RoleCollection is the collection storing role documents. Each document is
// shaped as “{name, permissions: [...], groups: [...]}“.
const RoleCollection = "role"

// GroupCollection is the collection storing server-managed user groups. Each
// document is shaped as “{name, description, members: [{username, method}]}“.
// Membership is folded into the resolving user's group set in Resolve so that
// granting a role to a group covers every member without an IdP.
const GroupCollection = "group"

// AllPermission is the wildcard permission that grants every action. It
// matches the Python codebase's "rw_all" semantics.
const AllPermission = "rw_all"

// ReadAllPermission is the read-side catch-all: it grants every `ro_*`
// permission and nothing more. It is the counterpart of AllPermission and
// matches what plugins.IsAuthorized already seeds its read set with, so a
// bespoke `ro_*` route and a plugin CRUD read route answer the same way for
// the same claim set.
const ReadAllPermission = "ro_all"

// readPermPrefix marks a permission as read-only, which is what
// ReadAllPermission stands in for.
const readPermPrefix = "ro_"

const (
	// PermReadTenant gates read access to the /api/v1/tenant registry. It is
	// evaluated against platform scope, independent of any tenant (D5).
	PermReadTenant = "ro_tenant"
	// PermWriteTenant gates CRUD on the /api/v1/tenant registry (D5).
	PermWriteTenant = "rw_tenant"
)

// PlatformAdminRole is the seeded role that holds PermReadTenant +
// PermWriteTenant. Assigned to the root user at first boot (D5).
const PlatformAdminRole = "platform_admin"

// RoleResolver expands an Identity into the role and permission lists used by
// the claims payload, by reading the role and user collections from db.Driver.
type RoleResolver struct {
	DB db.Driver
}

// NewRoleResolver wires a resolver to a driver.
func NewRoleResolver(driver db.Driver) *RoleResolver { return &RoleResolver{DB: driver} }

// Resolve gathers the effective roles and permissions for id. The resolution
// is the union of:
//
//   - roles explicitly attached to the user document (user.roles)
//   - roles whose “groups“ field intersects the identity's groups
//
// Permissions are the deduplicated union of each role's “permissions“ field.
// Missing collections are not fatal — the resolver returns empty slices.
func (r *RoleResolver) Resolve(ctx context.Context, id Identity) ([]string, []string, error) {
	if r.DB == nil {
		return nil, nil, errors.New("rbac: nil db driver")
	}

	roleSet := make(map[string]struct{})

	// 1. Roles explicitly attached to the user document.
	user, err := r.DB.GetOne(ctx, LocalCollection, db.Document{
		"name":   id.Username,
		"method": id.Method,
	})
	if err == nil && user != nil {
		for _, r := range stringSliceField(user, "roles") {
			roleSet[r] = struct{}{}
		}
		for _, r := range stringSliceField(user, "static_roles") {
			roleSet[r] = struct{}{}
		}
	}

	// 1.5. Server-managed groups: union the names of every group whose
	//      members[] contains this identity into the group set. This augments
	//      (does not replace) the IdP-supplied id.Groups, so a server-managed
	//      membership and an IdP group both resolve. The scan is non-fatal: on a
	//      fresh install the "group" collection may not exist yet.
	groupSet := make(map[string]struct{}, len(id.Groups))
	for _, g := range id.Groups {
		groupSet[g] = struct{}{}
	}
	for g := range r.serverManagedGroups(ctx, id.Username, id.Method) {
		groupSet[g] = struct{}{}
	}

	// 2. Group-mapped roles: scan the role collection for any document whose
	//    groups[] intersects the group set. We pull all roles and filter in Go;
	//    role counts are small (<100s) so the simple approach beats per-group
	//    queries.
	roles, _, err := r.DB.Search(ctx, RoleCollection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		return nil, nil, fmt.Errorf("rbac: search roles: %w", err)
	}

	permSet := make(map[string]struct{})
	for _, role := range roles {
		name, _ := role["name"].(string)
		if name == "" {
			continue
		}
		// Group match adds the role.
		for _, g := range stringSliceField(role, "groups") {
			if _, ok := groupSet[g]; ok {
				roleSet[name] = struct{}{}
				break
			}
		}
		// Once the role is in the set (for whatever reason) fold its perms.
		if _, ok := roleSet[name]; ok {
			for _, p := range stringSliceField(role, "permissions") {
				permSet[p] = struct{}{}
			}
		}
	}

	return sortedKeys(roleSet), sortedKeys(permSet), nil
}

// member is a single {username, method} entry in a group document's members[].
type member struct {
	Username string
	Method   string
}

// serverManagedGroups returns the set of group names whose members[] contains
// the given {username, method} identity. It scans the whole "group" collection
// and filters in Go — group counts per tenant are small (tens to low hundreds),
// matching the role-scan strategy. The incoming ctx is forwarded so the
// tenant-scoped driver isolates the scan by construction.
//
// A DB error is non-fatal: on a fresh install the collection may not exist yet,
// and a transient outage should degrade gracefully (the user keeps direct and
// IdP-group roles) rather than block login. The error is intentionally dropped.
func (r *RoleResolver) serverManagedGroups(ctx context.Context, username, method string) map[string]struct{} {
	out := make(map[string]struct{})
	// non-fatal: the "group" collection may not exist yet on a fresh install,
	// and a transient DB error must not block authentication.
	groups, _, _ := r.DB.Search(ctx, GroupCollection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	for _, g := range groups {
		name, _ := g["name"].(string)
		if name == "" {
			continue
		}
		for _, m := range toMemberSlice(g["members"]) {
			if m.Username == username && m.Method == method {
				out[name] = struct{}{}
				break
			}
		}
	}
	return out
}

// toMemberSlice coerces a group document's members[] field into []member,
// tolerating the post-JSON shape ([]any of map[string]any) the drivers return.
// Entries missing username or method are kept as-is (their blank field simply
// fails the identity match in serverManagedGroups).
func toMemberSlice(v any) []member {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]member, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		username, _ := m["username"].(string)
		method, _ := m["method"].(string)
		out = append(out, member{Username: username, Method: method})
	}
	return out
}

// IsReservedPlatformPerm reports whether p is a platform-only permission that a
// tenant-local role/user write must never carry. These permissions gate the
// /api/v1/tenant control plane (C5): folding them into a tenant role would let a
// tenant user self-escalate to platform admin on re-login.
func IsReservedPlatformPerm(p string) bool {
	return p == PermReadTenant || p == PermWriteTenant
}

// IsReservedPlatformRole reports whether name is a platform-only role that a
// tenant-local user write must never reference (via roles/static_roles) and a
// tenant-local role write must never create/name.
func IsReservedPlatformRole(name string) bool {
	return name == PlatformAdminRole
}

// RogueReservedRoles returns "tenant_id/name" identifiers of roles that carry a
// reserved platform permission (ro_tenant/rw_tenant) yet are NOT the seeded
// platform_admin role. These can only exist on databases written before the
// reserved-perm lock; the caller should WARN so an operator can remove them.
func RogueReservedRoles(ctx context.Context, driver db.Driver) ([]string, error) {
	if driver == nil {
		return nil, errors.New("rbac: nil db driver")
	}
	roles, _, err := driver.Search(ctx, RoleCollection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		return nil, fmt.Errorf("rbac: search roles: %w", err)
	}
	var rogue []string
	for _, r := range roles {
		name, _ := r["name"].(string)
		if IsReservedPlatformRole(name) {
			continue
		}
		for _, perm := range stringSliceField(r, "permissions") {
			if IsReservedPlatformPerm(perm) {
				tid, _ := r["tenant_id"].(string)
				rogue = append(rogue, tid+"/"+name)
				break
			}
		}
	}
	sort.Strings(rogue)
	return rogue, nil
}

// HasPermission returns true when the claim set carries the requested
// permission or a wildcard that covers it:
//
//   - AllPermission ("rw_all") covers everything.
//   - ReadAllPermission ("ro_all") covers a want with the "ro_" prefix, and
//     only that — it never satisfies a "rw_*" gate (including "rw_all") nor an
//     unprefixed permission name.
//
// The "ro_all" arm mirrors plugins.IsAuthorized, whose read set is seeded with
// both catch-alls: without it a read-only auditor could list a plugin's rows
// through the CRUD router yet get a 403 from a bespoke route gated on the same
// ro_* permission.
//
// Wildcards apply here only. The literal gates — HasLiteralPermission,
// middleware.RequireLiteralPerm and middleware.RequirePlatformPerm — take no
// catch-all, by design.
func HasPermission(claims snoozetypes.Claims, want string) bool {
	if want == "" {
		return true
	}
	readWanted := strings.HasPrefix(want, readPermPrefix)
	for _, p := range claims.Permissions {
		if p == AllPermission || p == want {
			return true
		}
		if readWanted && p == ReadAllPermission {
			return true
		}
	}
	return false
}

// HasLiteralPermission reports whether the claim set carries perm verbatim.
// Unlike HasPermission, the rw_all wildcard does NOT satisfy it — this mirrors
// the platform-gate semantics of RequirePlatformPerm and must be used for the
// reserved platform permissions (ro_tenant/rw_tenant).
func HasLiteralPermission(claims snoozetypes.Claims, perm string) bool {
	for _, p := range claims.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// sortedKeys turns a set into a deterministic slice. Stable output keeps
// audit logs and tests reproducible.
func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
