// Package widget implements the "widget" data-model plugin: stored
// dashboard widget definitions for the web UI. The widget-plugin discovery
// endpoints from the Python metadata (`/widget/plugin/...`) belong to the
// API layer and are not modelled here.
package widget

import (
	"context"
	_ "embed"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("widget", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for UI widgets.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "widget" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the widget plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for a widget document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":          map[string]any{"type": "string"},
			"vue_component": map[string]any{"type": "string"},
			"form":          map[string]any{"type": "object"},
			"options":       map[string]any{"type": "object"},
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map for widgets.
func (p *Plugin) Validate(_ map[string]any) error { return nil }
