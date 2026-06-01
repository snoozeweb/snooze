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

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
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

// Compile-time guarantees that the plugin keeps satisfying the optional
// interfaces the CRUD layer detects by assertion — so a signature drift fails
// the build rather than silently disabling the hook.
var (
	_ plugins.DataModel        = (*Plugin)(nil)
	_ plugins.WriteTransformer = (*Plugin)(nil)
	_ plugins.CreateHook       = (*Plugin)(nil)
)

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
			"user":       map[string]any{"type": "string"},
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

// TransformWrite stamps the authenticated principal onto a human-created
// comment so the alert timeline and dashboard activity feed can attribute it.
// `user` is server-authoritative — any client-supplied value is overwritten
// with the request's verified subject (whatever the auth method, including
// "anonymous" when that provider is enabled: an anonymous human action is still
// a human action, not a system event). `method` is only filled in from the
// claims when the caller didn't set one, so the chat-ops bridges
// (snooze-teams/-jira/-mcp), which post as a service account but record the
// originating channel in `method` ("teams"/"jira"/"mcp"), keep that channel.
// Auto-comments written by the aggregate-rule processor go straight to the
// driver and bypass this hook, so they alone stay user-less — and a user-less
// comment is exactly what the dashboard activity feed filters out as a system
// event.
func (p *Plugin) TransformWrite(ctx context.Context, doc map[string]any) error {
	if claims, ok := auth.ClaimsFrom(ctx); ok && claims.Subject != "" {
		doc["user"] = claims.Subject
		if m, _ := doc["method"].(string); m == "" {
			doc["method"] = claims.Method
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
