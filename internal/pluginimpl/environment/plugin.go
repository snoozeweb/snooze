// Package environment implements the "environment" data-model plugin:
// hierarchical environment definitions (production / staging / …) attached
// to records for filtering and routing.
package environment

import (
	"context"
	_ "embed"
	"errors"

	"github.com/japannext/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("environment", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for environment definitions.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "environment" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the environment plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for an environment document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"parent":     map[string]any{"type": "string"},
			"condition":  map[string]any{},
			"color":      map[string]any{"type": "string"},
			"tree_order": map[string]any{"type": "integer"},
		},
		"required":             []any{"name"},
		"additionalProperties": true,
	}
}

// Validate enforces the primary-key field on full writes; partial PATCH
// updates that omit name are tolerated.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if v, ok := obj["name"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("environment: name must not be empty")
		}
	}
	return nil
}
