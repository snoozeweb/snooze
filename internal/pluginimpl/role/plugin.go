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
//
// The reserved guard here is keyed off the document's own tenant_id field, so a
// stored role carrying a reserved platform permission (rw_tenant/ro_tenant) or
// named platform_admin is rejected unless the document is scoped to the default
// tenant. This is the structural check; the authoritative runtime guard lives
// in TransformWrite, which keys off the trusted request context rather than a
// client-supplied tenant_id.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("role: name must not be empty")
		}
	}
	// Determine scope from the doc's tenant_id (default tenant = platform path).
	tenantID, _ := obj["tenant_id"].(string)
	if tenantID == snoozetypes.DefaultTenant {
		return nil
	}
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
