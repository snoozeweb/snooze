// Package audit implements the "audit" data-model plugin. Audit events are
// produced by the audit middleware (internal/middleware/audit) — the plugin
// only owns the schema and exposes the read-side CRUD surface. Writes
// happen out-of-band; the API layer typically restricts POST/PATCH/DELETE
// to operators via the permission system.
package audit

import (
	"context"
	_ "embed"

	"github.com/japannext/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("audit", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for audit events.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "audit" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the audit plugin: events are not cached.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for an audit document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"object_type": map[string]any{"type": "string"},
			"object_uid":  map[string]any{"type": "string"},
			"action":      map[string]any{"type": "string"},
			"username":    map[string]any{"type": "string"},
			"method":      map[string]any{"type": "string"},
			"summary":     map[string]any{"type": "string"},
			"date_epoch":  map[string]any{"type": "number"},
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map; the middleware controls write shape.
func (p *Plugin) Validate(_ map[string]any) error { return nil }
