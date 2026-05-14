// Package stats implements the "stats" data-model plugin. Python exposes a
// precomputed time-series endpoint via a custom `StatsRoute`; the Go port
// will mount its own RouteProvider in a later phase. For Phase 4 this
// plugin ships as a plain DataModel so the orchestrator can wire the rest
// of the registry without a gap.
//
// TODO(phase-7): wire ComputeStats route (driver.ComputeStats is already
// implemented at the db layer; the plugin needs to add a RouteProvider that
// serves /api/v1/stats/<series> and /api/v1/stats/buckets).
package stats

import (
	"context"
	_ "embed"

	"github.com/japannext/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("stats", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for stored stats documents.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "stats" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: stats are aggregated on the fly by the driver.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for a stats counter document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":        map[string]any{"type": "string"},
			"value":      map[string]any{"type": "number"},
			"date_epoch": map[string]any{"type": "number"},
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map.
func (p *Plugin) Validate(_ map[string]any) error { return nil }
