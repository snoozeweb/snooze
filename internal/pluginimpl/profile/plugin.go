// Package profile implements the "profile" data-model plugin: per-user UI
// preferences and display metadata. Python persists profile records into
// sub-collections (`profile.general`, `profile.preferences`); the Go layer
// owns the parent `profile` collection here and lets section-aware routes
// (mounted by the API layer in a later phase) target sub-collections.
package profile

import (
	"context"
	_ "embed"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("profile", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for user profiles.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "profile" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the profile plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for a profile document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"method":       map[string]any{"type": "string"},
			"display_name": map[string]any{"type": "string"},
			"email":        map[string]any{"type": "string"},
			"preferences":  map[string]any{"type": "object"},
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map; the section-aware route layer
// performs structural checks.
func (p *Plugin) Validate(_ map[string]any) error { return nil }
