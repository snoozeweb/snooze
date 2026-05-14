// Package comment implements the "comment" data-model plugin: free-form
// notes attached to a record. The Python `_self` shorthand routes are part
// of the API layer's permission story and are not modelled here.
package comment

import (
	"context"
	_ "embed"
	"errors"

	"github.com/japannext/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("comment", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for record comments.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "comment" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op for the comment plugin.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for a comment document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"record_uid": map[string]any{"type": "string"},
			"name":       map[string]any{"type": "string"},
			"method":     map[string]any{"type": "string"},
			"message":    map[string]any{"type": "string"},
			"date":       map[string]any{"type": "string"},
		},
		"additionalProperties": true,
	}
}

// Validate enforces that a comment carries a non-empty message and references
// a record. Partial PATCH updates are tolerated.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	// Only enforce when the field is present — partial PATCH semantics.
	if v, ok := obj["message"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("comment: message must not be empty")
		}
	}
	if v, ok := obj["record_uid"]; ok {
		if s, _ := v.(string); s == "" {
			return errors.New("comment: record_uid must not be empty")
		}
	}
	return nil
}
