// Package group implements the "group" data-model plugin: server-managed user
// cohorts that the RBAC resolver folds into a user's group set at login. The
// resolution logic lives in internal/auth/rbac.go (RoleResolver); this plugin
// owns only the stored "group" collection and its generic CRUD surface.
package group

import (
	"context"
	_ "embed"
	"errors"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("group", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for server-managed user groups.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "group" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the group plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// PrimaryKey satisfies plugins.PrimaryKeyer. The tenant_id prefix ensures that
// groups with the same name in different tenants do not collide.
func (p *Plugin) PrimaryKey() []string { return []string{"tenant_id", "name"} }

// Schema returns the JSON Schema for a group document. Members are an array of
// {username, method} objects, mirroring the user collection's identity key.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"members": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"username": map[string]any{"type": "string"},
						"method":   map[string]any{"type": "string"},
					},
				},
			},
		},
		"required":             []any{"name"},
		"additionalProperties": true,
	}
}

// Validate enforces a non-empty name when the field is present. PATCH partials
// that omit the name are tolerated; there are no reserved-group concerns.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("group: name must not be empty")
		}
	}
	return nil
}
