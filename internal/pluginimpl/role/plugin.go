// Package role implements the "role" data-model plugin. Role membership and
// permission resolution live in internal/auth/*; this plugin owns only the
// stored-role collection and CRUD surface.
package role

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("role", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for stored roles.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "role" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the role plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// PrimaryKey satisfies plugins.PrimaryKeyer. The tenant_id prefix ensures
// that roles with the same name in different tenants do not collide.
func (p *Plugin) PrimaryKey() []string { return []string{"tenant_id", "name"} }

// Schema returns the JSON Schema for a role document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"permissions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"groups":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []any{"name"},
		"additionalProperties": true,
	}
}

// Validate enforces the primary-key field ([name]) on writes and applies the
// reserved-permission/role allowlist (C5). PATCH partials without a name field
// are tolerated.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("role: name must not be empty")
		}
	}
	// Reserved-perm/role lock (C5, hardened): no role written through the API may
	// carry a reserved platform permission or be named platform_admin, regardless
	// of tenant. The real platform_admin role is seeded by a direct driver write
	// (bypassing this plugin), so seeding is unaffected; this makes platform_admin
	// API-immutable and confines ro_tenant/rw_tenant to it.
	return checkReservedRole(obj)
}

// TransformWrite is the authoritative C5 guard. It runs after Validate with the
// trusted request context, so a tenant user cannot bypass the check by omitting
// or forging the tenant_id field on the body. Platform scope or the default
// tenant are exempt; any other tenant origin may not mint a role that carries a
// reserved platform permission or impersonates the platform_admin role.
func (p *Plugin) TransformWrite(ctx context.Context, doc map[string]any) error {
	if len(doc) == 0 {
		return nil
	}
	if snoozetypes.IsPlatformScope(ctx) {
		return nil
	}
	if tenantID, ok := snoozetypes.TenantFrom(ctx); ok && tenantID == snoozetypes.DefaultTenant {
		return nil
	}
	return checkReservedRole(doc)
}

// checkReservedRole rejects a role document that names the platform_admin role
// or folds a reserved platform permission.
func checkReservedRole(obj map[string]any) error {
	if name, _ := obj["name"].(string); auth.IsReservedPlatformRole(name) {
		return fmt.Errorf("role: %q is a reserved platform role and cannot be created or named by a tenant", name)
	}
	for _, perm := range stringSlice(obj["permissions"]) {
		if auth.IsReservedPlatformPerm(perm) {
			return fmt.Errorf("role: permission %q is reserved for the platform control plane and cannot be granted to a tenant role", perm)
		}
	}
	return nil
}

// GuardWrite makes the reserved platform_admin role API-immutable. It may not be
// created, named, or edited through the CRUD API — including adding a `groups`
// entry that the RBAC resolver would group-map users into, or changing its
// permissions. Only the bootstrap path (a direct driver write under platform
// scope) writes it. Closes the group-mapping escalation route into rw_tenant.
func (p *Plugin) GuardWrite(ctx context.Context, uid string, doc map[string]any) error {
	if snoozetypes.IsPlatformScope(ctx) {
		return nil
	}
	if name, _ := doc["name"].(string); auth.IsReservedPlatformRole(name) {
		return fmt.Errorf("role %q is reserved platform infrastructure and cannot be created or modified", auth.PlatformAdminRole)
	}
	if uid != "" {
		if d, err := p.host.DB().GetOne(ctx, "role", db.Document{"uid": uid}); err == nil && d != nil {
			if name, _ := d["name"].(string); auth.IsReservedPlatformRole(name) {
				return fmt.Errorf("role %q is reserved platform infrastructure and cannot be modified", auth.PlatformAdminRole)
			}
		}
	}
	return nil
}

// GuardDelete blocks deletion of the reserved platform_admin role; it is seeded
// infrastructure and must remain (every holder would otherwise lose rw_tenant).
func (p *Plugin) GuardDelete(ctx context.Context, uids []string) error {
	if snoozetypes.IsPlatformScope(ctx) {
		return nil
	}
	for _, uid := range uids {
		doc, err := p.host.DB().GetOne(ctx, "role", db.Document{"uid": uid})
		if err != nil || doc == nil {
			continue
		}
		if name, _ := doc["name"].(string); auth.IsReservedPlatformRole(name) {
			return fmt.Errorf("role %q is reserved platform infrastructure and cannot be deleted", name)
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
