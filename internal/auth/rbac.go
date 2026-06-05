package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// RoleCollection is the collection storing role documents. Each document is
// shaped as “{name, permissions: [...], groups: [...]}“.
const RoleCollection = "role"

// AllPermission is the wildcard permission that grants every action. It
// matches the Python codebase's "rw_all" semantics.
const AllPermission = "rw_all"

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

	// 2. Group-mapped roles: scan the role collection for any document whose
	//    groups[] intersects id.Groups. We pull all roles and filter in Go;
	//    role counts are small (<100s) so the simple approach beats per-group
	//    queries.
	roles, _, err := r.DB.Search(ctx, RoleCollection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		return nil, nil, fmt.Errorf("rbac: search roles: %w", err)
	}

	groupSet := make(map[string]struct{}, len(id.Groups))
	for _, g := range id.Groups {
		groupSet[g] = struct{}{}
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

// HasPermission returns true when the claim set carries either the requested
// permission or the AllPermission wildcard.
func HasPermission(claims snoozetypes.Claims, want string) bool {
	if want == "" {
		return true
	}
	for _, p := range claims.Permissions {
		if p == AllPermission || p == want {
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
