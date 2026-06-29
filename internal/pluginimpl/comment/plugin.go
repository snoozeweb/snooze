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
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config/schema"
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

// New constructs a comment Plugin instance. It is exported so sibling packages
// (the Plan 36 chataction helper's tests) can wire a real comment plugin into a
// test host and exercise the genuine create seam (TransformWrite + GuardWrite +
// AfterCreate) rather than re-implementing the state-transition mechanics.
func New(meta plugins.Metadata) (plugins.Plugin, error) {
	return factory(meta)
}

// Plugin is the data-model plugin for record comments.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
	// clock supplies "now" for the timed-lifecycle deadline stamping in
	// AfterCreate. Overrideable for deterministic tests; defaults to time.Now
	// in PostInit so production never reads a zero clock. Mirrors the
	// aggregaterule plugin's injected clock — the plugins.Host interface
	// deliberately exposes no clock, so each plugin owns its own.
	clock func() time.Time
}

// Compile-time guarantees that the plugin keeps satisfying the optional
// interfaces the CRUD layer detects by assertion — so a signature drift fails
// the build rather than silently disabling the hook.
var (
	_ plugins.DataModel        = (*Plugin)(nil)
	_ plugins.WriteTransformer = (*Plugin)(nil)
	_ plugins.CreateHook       = (*Plugin)(nil)
	_ plugins.WriteGuard       = (*Plugin)(nil)
)

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "comment" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.clock == nil {
		p.clock = time.Now
	}
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

// GuardWrite vetoes a state-transition comment whose action is not legal from
// the linked record's current state, BEFORE the comment is written. It runs
// only for state-changing comment types ({ack,close,open,esc}); free-form
// comments, orphan comments (no record_uid), and missing records all pass
// through (fail-open). A non-nil error aborts the create with HTTP 403.
func (p *Plugin) GuardWrite(ctx context.Context, _ string, doc map[string]any, _ bool) error {
	action, _ := doc["type"].(string)
	if !stateChangingActions[action] {
		return nil
	}
	uid, _ := doc["record_uid"].(string)
	if uid == "" || p.host == nil {
		return nil
	}
	rec, err := p.host.DB().GetOne(ctx, "record", db.Document{"uid": uid})
	if err != nil {
		// Fail-open: a missing/unreadable record is caught later in AfterCreate.
		return nil
	}
	currentState, _ := rec["state"].(string)
	return ValidateTransition(currentState, action)
}

// AfterCreate applies side effects after each comment is written:
//   - For comments with type ∈ {"ack","close","open","esc"}, updates the
//     linked record's `state` field to match and stamps the timed-lifecycle
//     deadlines (ack_until / escalate_at).
//   - Denormalises the acknowledger onto the record as `acked_by`: stamped
//     from the comment's resolved `user` on `ack`, removed entirely on
//     `open`/`close`, and left untouched on `esc` (the last acknowledger is
//     kept for accountability through a re-escalation).
//   - Increments the record's `comment_count` field by 1.
//
// Errors looking up or writing to the record collection are returned so
// the CRUD layer can log them. The comment itself stays written.
func (p *Plugin) AfterCreate(ctx context.Context, docs []map[string]any) error {
	if p.host == nil {
		return nil
	}
	for _, doc := range docs {
		uid, _ := doc["record_uid"].(string)
		if uid == "" {
			continue
		}
		patch := db.Document{}
		commentType, _ := doc["type"].(string)
		if t := commentType; stateChangingActions[t] {
			// Most actions are their own state; the timed-shelve pair maps to a
			// distinct state ("shelve"→"shelved", "unshelve"→"open").
			targetState := t
			if s, ok := stateForAction[t]; ok {
				targetState = s
			}
			patch["state"] = targetState
			// Stamp the server-controlled timed-lifecycle deadlines, mirroring
			// Alerta's timeout.py server-override. now comes from the injected
			// clock (never time.Now() in this core path); the timeouts are read
			// live from the runtime settings (falling back to the file-config
			// baseline). The escalate-timeout / unshelve-timeout housekeeper
			// sweeps enforce these.
			ackTimeout, escalateAfter := p.lifecycleTimeouts(ctx)
			now := p.now().Unix()
			switch t {
			case "ack":
				// An ack pauses escalation and arms an expiry.
				patch["ack_until"] = now + int64(ackTimeout.Seconds())
				patch["escalate_at"] = int64(0)
				// Lifting any timed shelve: an explicit ack supersedes it.
				patch["shelve_until"] = int64(0)
				// Denormalise the acknowledger onto the record for the alert-list
				// "Acked by" column. doc["user"] is server-authoritative by now
				// (TransformWrite stamped it from the JWT claims). A user-less ack
				// (auto-comment that bypasses TransformWrite) leaves acked_by unset
				// so omitempty/EXISTS stays clean.
				if user, _ := doc["user"].(string); user != "" {
					patch["acked_by"] = user
				}
			case "open", "esc":
				// Reopened/escalated: clear the ack expiry and (re-)arm the
				// escalation deadline so a reverted alert escalates again.
				patch["ack_until"] = int64(0)
				if escalateAfter > 0 {
					patch["escalate_at"] = now + int64(escalateAfter.Seconds())
				} else {
					patch["escalate_at"] = int64(0)
				}
				// Reopening also lifts any timed shelve.
				patch["shelve_until"] = int64(0)
			case "close":
				// Terminal: nothing left to expire, escalate, or unshelve.
				patch["ack_until"] = int64(0)
				patch["escalate_at"] = int64(0)
				patch["shelve_until"] = int64(0)
			case "shelve":
				// Park the alert in "shelved" and stamp the auto-return deadline.
				// The unshelve-timeout sweep reverts it once now passes this.
				patch["shelve_until"] = now + int64(p.shelveTimeout(ctx).Seconds())
			case "unshelve":
				// Explicitly lifting the timed shelve clears the deadline.
				patch["shelve_until"] = int64(0)
			}
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
		// Re-opening or closing clears the acknowledger. UnsetFields truly
		// deletes the key (vs. an UpdateOne zero-value that would leave an empty
		// string), so omitempty/EXISTS stops matching and the "Acked by" column
		// renders "—". Runs after the UpdateOne above, which already applied the
		// state + ack_until/escalate_at clearing for this transition.
		if commentType == "open" || commentType == "close" {
			if _, err := p.host.DB().UnsetFields(ctx, "record", []string{"acked_by"},
				condition.Equals("uid", uid)); err != nil {
				return fmt.Errorf("comment: unset acked_by on record %s: %w", uid, err)
			}
		}
	}
	return nil
}

// now returns the current time from the injected clock, defaulting to time.Now
// when the plugin was constructed without PostInit (defensive).
func (p *Plugin) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// lifecycleTimeouts resolves the live ack/escalate timeouts the state-transition
// stamping uses. Preference order: the process-wide RuntimeSettings (fully
// live, tenant-aware) when the host exposes it, then the file-config baseline
// (host.Config().Housekeeper), then the schema defaults. This keeps the
// observable behaviour identical to the housekeeper sweep, which reads the same
// RuntimeSettings.
func (p *Plugin) lifecycleTimeouts(ctx context.Context) (ackTimeout, escalateAfter time.Duration) {
	def := schema.DefaultHousekeeper()
	ackTimeout = def.AckTimeout.AsDuration()
	escalateAfter = def.EscalateAfter.AsDuration()

	if p.host == nil {
		return ackTimeout, escalateAfter
	}
	// Live runtime settings (preferred): tenant-scoped, DB-overridable.
	if rsh, ok := p.host.(plugins.RuntimeSettingsHost); ok {
		if rs := rsh.RuntimeSettings(); rs != nil {
			if hk, err := rs.Housekeeper(ctx); err == nil {
				if d := hk.AckTimeout.AsDuration(); d > 0 {
					ackTimeout = d
				}
				escalateAfter = hk.EscalateAfter.AsDuration()
				return ackTimeout, escalateAfter
			}
		}
	}
	// Fall back to the file-config baseline.
	if cfg := p.host.Config(); cfg != nil {
		if d := cfg.Housekeeper.AckTimeout.AsDuration(); d > 0 {
			ackTimeout = d
		}
		escalateAfter = cfg.Housekeeper.EscalateAfter.AsDuration()
	}
	return ackTimeout, escalateAfter
}

// shelveTimeout resolves the live timed-shelve window the shelve stamping uses,
// with the same preference order as lifecycleTimeouts: process-wide
// RuntimeSettings (tenant-aware, DB-overridable), then the file-config
// baseline, then the schema default (4h). The unshelve-timeout housekeeper
// sweep reads the same RuntimeSettings, keeping the stamped deadline consistent
// with enforcement.
func (p *Plugin) shelveTimeout(ctx context.Context) time.Duration {
	timeout := schema.DefaultShelveTimeout
	if p.host == nil {
		return timeout
	}
	if rsh, ok := p.host.(plugins.RuntimeSettingsHost); ok {
		if rs := rsh.RuntimeSettings(); rs != nil {
			if hk, err := rs.Housekeeper(ctx); err == nil {
				if d := hk.ShelveTimeout.AsDuration(); d > 0 {
					return d
				}
				return timeout
			}
		}
	}
	if cfg := p.host.Config(); cfg != nil {
		if d := cfg.Housekeeper.ShelveTimeout.AsDuration(); d > 0 {
			return d
		}
	}
	return timeout
}
