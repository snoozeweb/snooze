// Package role implements the "role" data-model plugin. Role membership and
// permission resolution live in internal/auth/*; this plugin owns only the
// stored-role collection and CRUD surface.
package role

import (
	"context"
	_ "embed"
	"errors"

	"github.com/snoozeweb/snooze/internal/plugins"
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

// PrimaryKey lets the generic CRUD createHandler reject duplicates whose
// `name` already exists in the collection. Mirrors metadata.yaml's
// route_defaults.primary in the Python codebase.
func (p *Plugin) PrimaryKey() []string { return []string{"name"} }

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

// Validate enforces the primary-key field ([name]) on writes. PATCH partials
// without a name field are tolerated.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("role: name must not be empty")
		}
	}
	return nil
}
