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

	"github.com/japannext/snooze/internal/plugins"
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
			"last_login":   map[string]any{"type": "string"},
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
	return nil
}
