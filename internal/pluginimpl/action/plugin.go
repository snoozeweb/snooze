// Package action implements the "action" data-model plugin: stored action
// definitions referenced by notifications. The notification plugin resolves
// a record's matching action document and invokes the configured notifier
// (mail, webhook, script …). Delayed / batched execution lives in the
// notification subsystem (Phase 5+); this plugin only owns the action
// collection schema.
package action

import (
	"context"
	_ "embed"
	"errors"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("action", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for action definitions.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "action" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: action definitions are read on demand by the
// notification subsystem.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for an action document.
//
// `action.selected` carries the notifier plugin name (mail, webhook, …);
// `action.subcontent` carries the per-plugin payload. Mirrors the Python
// ActionObject reconstruction layout.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"action": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selected":   map[string]any{"type": "string"},
					"subcontent": map[string]any{"type": "object"},
				},
			},
			"batch":         map[string]any{"type": "boolean"},
			"batch_timer":   map[string]any{"type": "integer"},
			"batch_maxsize": map[string]any{"type": "integer"},
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
			return errors.New("action: name must not be empty")
		}
	}
	return nil
}
