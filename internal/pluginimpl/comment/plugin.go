// Package comment implements the "comment" data-model plugin: free-form
// notes attached to a record. POST /api/v1/comment also applies a state
// transition to the linked record when the comment's `type` is one of
// "ack", "close", "open", or "esc" — mirroring the legacy Python route.
package comment

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/japannext/snooze/internal/db"
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
			"type":       map[string]any{"type": "string"},
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

// AfterCreate applies side effects after each comment is written:
//   - For comments with type ∈ {"ack","close","open","esc"}, updates the
//     linked record's `state` field to match.
//   - Increments the record's `comment_count` field by 1.
//
// Errors looking up or writing to the record collection are returned so
// the CRUD layer can log them. The comment itself stays written.
func (p *Plugin) AfterCreate(ctx context.Context, docs []map[string]any) error {
	if p.host == nil {
		return nil
	}
	stateChanging := map[string]bool{"ack": true, "close": true, "open": true, "esc": true}

	for _, doc := range docs {
		uid, _ := doc["record_uid"].(string)
		if uid == "" {
			continue
		}
		patch := db.Document{}
		if t, ok := doc["type"].(string); ok && stateChanging[t] {
			patch["state"] = t
		}
		rec, err := p.host.DB().GetOne(ctx, "record", db.Document{"uid": uid})
		if err != nil {
			return fmt.Errorf("comment: lookup record %s: %w", uid, err)
		}
		current, _ := rec["comment_count"].(int64)
		if c2, ok := rec["comment_count"].(int); ok && current == 0 {
			current = int64(c2)
		}
		if c3, ok := rec["comment_count"].(float64); ok && current == 0 {
			current = int64(c3)
		}
		patch["comment_count"] = current + 1
		if err := p.host.DB().UpdateOne(ctx, "record", uid, patch, true); err != nil {
			return fmt.Errorf("comment: update record %s: %w", uid, err)
		}
	}
	return nil
}
