// Package record implements the "record" data-model plugin: the alerts
// collection. Records are written by the processing pipeline; this plugin
// owns the schema and exposes the canonical CRUD surface via
// plugins.MountCRUD.
package record

import (
	"context"
	_ "embed"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("record", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for the alerts collection. It implements
// plugins.Plugin and plugins.DataModel.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "record" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls. There is no initial
// state to load — the pipeline writes records as they arrive.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: records are not cached in memory.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema descriptor for record documents.
// Currently a permissive any-object placeholder; the typed schema lives in
// pkg/snoozetypes and will be wired in a later phase.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map. Pipeline-side validation already
// ensures records are sane before they reach the CRUD layer; client-side
// writes are intentionally permissive so the UI can post partial drafts.
func (p *Plugin) Validate(_ map[string]any) error { return nil }
