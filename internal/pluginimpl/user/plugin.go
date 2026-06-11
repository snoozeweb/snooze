// Package user implements the "user" data-model plugin.
//
// Authentication logic — including the user/role/profile reconciliation that
// the Python “manage_db“ helper performs — lives in internal/auth/*. This
// plugin is intentionally a thin DataModel; it owns the collection schema
// and the CRUD surface, nothing more.
package user

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("user", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for stored users.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "user" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host. The user collection has no in-memory cache.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the user plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// PrimaryKey satisfies plugins.PrimaryKeyer. The tenant_id prefix ensures
// that users with the same (name, method) in different tenants do not collide.
func (p *Plugin) PrimaryKey() []string { return []string{"tenant_id", "name", "method"} }

// Schema returns the JSON Schema for a user document. Mirrors the Python
// route_defaults.primary ([name, method]) plus the conventional fields.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"method":       map[string]any{"type": "string"},
			"groups":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"roles":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"static_roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			// enabled gates login: a disabled user is rejected at the login and
			// refresh paths (see internal/auth/local.go + internal/api/routes_login.go).
			"enabled":      map[string]any{"type": "boolean"},
			"last_login":   map[string]any{"type": "number"},
			"created_at":   map[string]any{"type": "number"},
			"display_name": map[string]any{"type": "string"},
			"email":        map[string]any{"type": "string"},
		},
		"additionalProperties": true,
	}
}

// Validate enforces the primary-key fields (name, method) on writes. Empty
// patches are tolerated because PATCH partial updates are legitimate.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("user: name must not be empty")
		}
	}
	if v, ok := obj["method"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("user: method must not be empty")
		}
	}
	// Reserved-role allowlist (C5): a tenant-local user must not reference the
	// platform_admin role. Scope is read from the doc's tenant_id here (default
	// tenant = platform path); the authoritative guard keyed off the trusted
	// request context lives in TransformWrite.
	tenantID, _ := obj["tenant_id"].(string)
	if tenantID == snoozetypes.DefaultTenant {
		return nil
	}
	return checkReservedUserRoles(obj)
}

// checkReservedUserRoles rejects a user document that references the
// platform_admin role via roles or static_roles.
func checkReservedUserRoles(obj map[string]any) error {
	for _, field := range []string{"roles", "static_roles"} {
		for _, role := range stringSlice(obj[field]) {
			if auth.IsReservedPlatformRole(role) {
				return fmt.Errorf("user: role %q is reserved for the platform control plane and cannot be assigned to a tenant user", role)
			}
		}
	}
	return nil
}

// stringSlice coerces a JSON array field (decoded as []any or []string) into a
// []string, ignoring non-string elements.
func stringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// GuardWrite enforces platform-admin integrity on user writes (C5, hardened),
// running with the trusted request context before the DB write. Granting or
// removing the platform_admin role requires the caller to hold a *literal*
// rw_tenant permission (rw_all does NOT count); a caller may never strip
// platform_admin from their own account; and the system must always retain at
// least one enabled platform_admin holder.
func (p *Plugin) GuardWrite(ctx context.Context, uid string, doc map[string]any, replace bool) error {
	if snoozetypes.IsPlatformScope(ctx) {
		return nil
	}
	var prior map[string]any
	if uid != "" {
		if d, err := p.host.DB().GetOne(ctx, auth.LocalCollection, db.Document{"uid": uid}); err == nil && d != nil {
			prior = d
		}
	}

	priorAdmin := docHasPlatformAdmin(prior)
	newAdmin := newHasPlatformAdmin(doc, prior, replace)
	claims, _ := auth.ClaimsFrom(ctx)

	if newAdmin != priorAdmin {
		if !auth.HasLiteralPermission(claims, auth.PermWriteTenant) {
			return fmt.Errorf("user: assigning or removing the %q role requires the %q permission",
				auth.PlatformAdminRole, auth.PermWriteTenant)
		}
		if !newAdmin { // removal
			if isSelf(claims, prior) {
				return fmt.Errorf("user: cannot remove the %q role from your own account", auth.PlatformAdminRole)
			}
			if other, err := enabledPlatformAdminExists(ctx, p.host.DB(), map[string]struct{}{uid: {}}); err != nil {
				return fmt.Errorf("user: platform-admin invariant check: %w", err)
			} else if !other {
				return errors.New("user: cannot remove the last platform admin")
			}
		}
	}

	if priorAdmin && disablesUser(doc, prior) {
		if other, err := enabledPlatformAdminExists(ctx, p.host.DB(), map[string]struct{}{uid: {}}); err != nil {
			return fmt.Errorf("user: platform-admin invariant check: %w", err)
		} else if !other {
			return errors.New("user: cannot disable the last platform admin")
		}
	}
	return nil
}

// docHasPlatformAdmin reports whether a user doc references platform_admin via
// roles or static_roles.
func docHasPlatformAdmin(doc map[string]any) bool {
	if doc == nil {
		return false
	}
	for _, field := range []string{"roles", "static_roles"} {
		for _, r := range stringSlice(doc[field]) {
			if auth.IsReservedPlatformRole(r) {
				return true
			}
		}
	}
	return false
}

// newHasPlatformAdmin computes post-write platform_admin membership. For a PATCH
// (replace=false) an absent roles/static_roles field inherits the prior value
// (merge semantics); for a PUT (replace=true) the body is authoritative, so an
// absent field means the role set is being cleared.
func newHasPlatformAdmin(doc, prior map[string]any, replace bool) bool {
	effective := map[string]any{}
	for _, field := range []string{"roles", "static_roles"} {
		if _, ok := doc[field]; ok {
			effective[field] = doc[field]
		} else if !replace && prior != nil {
			effective[field] = prior[field]
		}
	}
	return docHasPlatformAdmin(effective)
}

// isSelf reports whether prior is the caller's own user record.
func isSelf(claims snoozetypes.Claims, prior map[string]any) bool {
	if prior == nil {
		return false
	}
	name, _ := prior["name"].(string)
	method, _ := prior["method"].(string)
	tenant, _ := prior["tenant_id"].(string)
	if tenant == "" {
		tenant = snoozetypes.DefaultTenant
	}
	ct := claims.TenantID
	if ct == "" {
		ct = snoozetypes.DefaultTenant
	}
	return claims.Subject == name && claims.Method == method && ct == tenant
}

// disablesUser reports whether the write turns a previously-enabled user off.
func disablesUser(doc, prior map[string]any) bool {
	v, ok := doc["enabled"]
	if !ok {
		return false
	}
	if enabled, _ := v.(bool); enabled {
		return false
	}
	if prior == nil {
		return true
	}
	if pe, ok := prior["enabled"].(bool); ok {
		return pe // only an enabled->disabled transition counts
	}
	return true // absent prior enabled is treated as enabled
}

// enabledPlatformAdminExists reports whether any enabled platform_admin holder
// exists in the current tenant scope whose uid is NOT in exclude.
func enabledPlatformAdminExists(ctx context.Context, drv db.Driver, exclude map[string]struct{}) (bool, error) {
	docs, _, err := drv.Search(ctx, auth.LocalCollection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		return false, err
	}
	for _, d := range docs {
		if u, _ := d["uid"].(string); u != "" {
			if _, skip := exclude[u]; skip {
				continue
			}
		}
		if en, ok := d["enabled"].(bool); ok && !en {
			continue
		}
		if docHasPlatformAdmin(d) {
			return true, nil
		}
	}
	return false, nil
}

// GuardDelete blocks deleting the last enabled platform_admin holder, including
// the case where a bulk delete would remove every holder at once.
func (p *Plugin) GuardDelete(ctx context.Context, uids []string) error {
	if snoozetypes.IsPlatformScope(ctx) {
		return nil
	}
	deleting := make(map[string]struct{}, len(uids))
	for _, u := range uids {
		deleting[u] = struct{}{}
	}
	survives, err := enabledPlatformAdminExists(ctx, p.host.DB(), deleting)
	if err != nil {
		return fmt.Errorf("user: platform-admin invariant check: %w", err)
	}
	if survives {
		return nil
	}
	// No holder survives the batch: block iff the batch actually removes one.
	for _, uid := range uids {
		d, err := p.host.DB().GetOne(ctx, auth.LocalCollection, db.Document{"uid": uid})
		if err != nil || d == nil {
			continue
		}
		if en, ok := d["enabled"].(bool); ok && !en {
			continue
		}
		if docHasPlatformAdmin(d) {
			return errors.New("user: cannot delete the last platform admin")
		}
	}
	return nil
}

// TransformWrite intercepts the `password` field on POST/PUT/PATCH bodies.
//
//   - An absent `password` is left as-is (PATCH partial updates rely on this).
//   - An empty-string `password` is dropped so an admin editing other fields
//     does not accidentally clear the credential.
//   - A non-empty plaintext value is bcrypt-hashed and written back to the
//     same field, so the document persisted by the CRUD layer never carries
//     plaintext.
//
// Mirrors the Python 1.x UserRoute.update_password helper, except the hash
// lives on the user document itself rather than a separate user.password
// collection (see internal/auth/local.go for the collapsed shape).
func (p *Plugin) TransformWrite(ctx context.Context, doc map[string]any) error {
	// Authoritative C5 guard: a tenant-local user write may not reference the
	// platform_admin role. Scope is taken from the trusted request context, so
	// a forged/omitted tenant_id field on the body cannot bypass it. Platform
	// scope and the default tenant are exempt.
	if !snoozetypes.IsPlatformScope(ctx) {
		if tenantID, ok := snoozetypes.TenantFrom(ctx); !ok || tenantID != snoozetypes.DefaultTenant {
			if err := checkReservedUserRoles(doc); err != nil {
				return err
			}
		}
	}

	raw, present := doc["password"]
	if !present {
		return nil
	}
	plaintext, _ := raw.(string)
	if plaintext == "" {
		delete(doc, "password")
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: hash password: %w", err)
	}
	doc["password"] = string(hash)
	// A password write only makes sense against a local-method user. When
	// the body carries an explicit method we enforce that; when it doesn't
	// (PATCH) the existing document's method is trusted.
	if m, ok := doc["method"].(string); ok && m != "" && m != auth.LocalMethod {
		return fmt.Errorf("user: cannot set password on %q-method user", m)
	}
	return nil
}
