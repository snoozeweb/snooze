// Package comment implements the "comment" data-model plugin: free-form
// notes attached to a record. POST /api/v1/comment also applies a state
// transition to the linked record when the comment's `type` is one of
// "ack", "close", "open", or "esc" — mirroring the legacy Python route — and
// maintains the record's ownership (internal/ownership): ack and close make
// the caller the owner, open and esc clear it, and the two ownership-only
// types "assign" (make `assignee` the owner) and "release" (clear the owner)
// change it without a transition of their own.
package comment

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/resolutionhold"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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
			// Operator-chosen shelve window in seconds. Only meaningful on a
			// `type: "shelve"` comment, where AfterCreate stamps
			// shelve_until = now + duration; absent/zero falls back to the
			// configured housekeeping.shelve_timeout.
			"duration": map[string]any{"type": "integer", "minimum": 0},
			// The new owner's login and auth method on a `type: "assign"`
			// comment. assignee_method may be omitted when the login is
			// unambiguous in the tenant; GuardWrite fills it in.
			"assignee":        map[string]any{"type": "string"},
			"assignee_method": map[string]any{"type": "string"},
			// The tool or agent that posted the comment on the user's behalf
			// ("snooze-skill", "alert-rca"), like agentic.source. Client-supplied
			// and informational: `user` stays the authoritative actor.
			"source": map[string]any{"type": "string", "maxLength": snoozetypes.MaxSourceLen},
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
	if v, ok := obj["source"]; ok && v != nil {
		src, isString := v.(string)
		switch {
		case !isString:
			return errors.New("comment: source must be a string")
		case utf8.RuneCountInString(src) > snoozetypes.MaxSourceLen:
			return fmt.Errorf("comment: source must be at most %d characters", snoozetypes.MaxSourceLen)
		case strings.ContainsRune(src, 0):
			return errors.New("comment: source must not contain the NUL character")
		}
	}
	// An assign names its new owner; without one there is nothing to assign.
	// Only checked when the type is present (partial PATCH semantics).
	if t, _ := obj["type"].(string); t == "assign" {
		if s, _ := obj["assignee"].(string); s == "" {
			return errors.New("comment: assign requires an assignee")
		}
	}
	// A shelve window is a forward-looking deadline: a negative one would stamp
	// shelve_until in the past and the unshelve sweep would revert the record on
	// its next tick, which is not what the operator asked for. There is no upper
	// bound on purpose — a very long shelve is a legitimate choice.
	if v, ok := obj["duration"]; ok && v != nil {
		d, numeric := asSeconds(v)
		if !numeric {
			return errors.New("comment: duration must be a number of seconds")
		}
		if d < 0 {
			return errors.New("comment: duration must not be negative")
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
// only for state-changing comment types ({ack,close,open,esc}) and the
// ownership types ({assign,release}, see guardOwnership); free-form comments,
// orphan comments (no record_uid), and missing records all pass through
// (fail-open). A non-nil error aborts the create with HTTP 403.
func (p *Plugin) GuardWrite(ctx context.Context, commentUID string, doc map[string]any, _ bool) error {
	action, _ := doc["type"].(string)
	if !stateChangingActions[action] && !ownershipActions[action] {
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
	if ownershipActions[action] {
		// Only a create applies an ownership change (AfterCreate is the only
		// hook that acts on it), so only a create is held to its
		// preconditions; editing an old assign comment's message later must
		// not fail because the record has since been closed.
		if commentUID != "" {
			return nil
		}
		return p.guardOwnership(ctx, action, rec, doc)
	}
	currentState, _ := rec["state"].(string)
	return ValidateTransition(currentState, action)
}

// guardOwnership holds an assign/release comment to its preconditions:
//
//   - assign is refused on a closed record (there is nothing left to work on)
//     and when no enabled user with the assignee's login — and method, when
//     given — exists in the caller's tenant. The lookup runs under the request
//     context, so the driver's tenant scoping confines it to that tenant. An
//     omitted assignee_method is filled in on doc from the one matching user,
//     so the comment written right after, and AfterCreate, carry it; a login
//     shared by several methods must name one.
//   - release is refused on a record with no owner.
func (p *Plugin) guardOwnership(ctx context.Context, action string, rec db.Document, doc map[string]any) error {
	switch action {
	case "assign":
		if state, _ := rec["state"].(string); state == "close" {
			return fmt.Errorf("%w: %q on a closed record", ErrInvalidTransition, action)
		}
		name, _ := doc["assignee"].(string)
		method, _ := doc["assignee_method"].(string)
		users, _, err := p.host.DB().Search(ctx, "user", ownership.UserCond(name), db.Page{})
		if err != nil {
			return fmt.Errorf("comment: look up assignee %q: %w", name, err)
		}
		resolved, err := ownership.MatchAssignee(users, name, method)
		if err != nil {
			return fmt.Errorf("comment: assign to %q: %w", name, err)
		}
		doc["assignee_method"] = resolved
	case "release":
		if !ownership.IsOwned(rec) {
			return fmt.Errorf("%w: %q on a record with no owner", ErrInvalidTransition, action)
		}
	}
	return nil
}

// AfterCreate applies side effects after each comment is written:
//   - For comments with type ∈ {"ack","close","open","esc"}, updates the
//     linked record's `state` field to match and stamps the timed-lifecycle
//     deadlines (ack_until / escalate_at).
//   - Denormalises the acknowledger onto the record as `acked_by`: stamped
//     from the comment's resolved `user` on `ack`, removed entirely on
//     `open`/`close`, and left untouched on `esc` (the last acknowledger is
//     kept for accountability through a re-escalation).
//   - Maintains ownership (see applyOwnership): `ack`/`close` take it for the
//     comment's user, `open`/`esc` clear it, `assign` takes it for the
//     assignee and `release` clears it — returning an acknowledged record to
//     `open` (same timers as `open`, `acked_by` removed). Neither ownership
//     type re-notifies.
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
		// now comes from the injected clock (never time.Now() in this core
		// path); both the timed-lifecycle deadlines and the escalation stamp
		// below use it.
		now := p.now().Unix()
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
				// Reopened/escalated: clear the ack expiry, (re-)arm the
				// escalation deadline so a reverted alert escalates again, and
				// lift any timed shelve. Shared with `release` of an ack, which
				// returns the record to open on the same clock.
				for k, v := range ownership.ReopenTimers(now, escalateAfter) {
					patch[k] = v
				}
				// Back in play: any resolution hold from an earlier close ends.
				for k, v := range resolutionhold.Clear() {
					patch[k] = v
				}
			case "close":
				// Terminal: nothing left to expire, escalate, or unshelve.
				patch["ack_until"] = int64(0)
				patch["escalate_at"] = int64(0)
				patch["shelve_until"] = int64(0)
				// A human close arms the resolution hold, so a source that keeps
				// firing for a while after the fix does not re-open the alert and
				// page again (internal/resolutionhold). The verdict is checked on
				// the re-fire, not here: the analysis may be marked resolved just
				// before or just after the close. A user-less close (an
				// auto-comment) arms nothing.
				hold := time.Duration(0)
				if user, _ := doc["user"].(string); user != "" {
					hold = p.resolutionHold(ctx)
				}
				for k, v := range resolutionhold.Start(now, hold) {
					patch[k] = v
				}
			case "shelve":
				// Park the alert in "shelved" and stamp the auto-return deadline.
				// The unshelve-timeout sweep reverts it once now passes this.
				// An operator-chosen `duration` (seconds) wins over the
				// configured housekeeping.shelve_timeout, so the window picked
				// in the shelve dialog is the window that is actually served;
				// absent or zero falls back to the configured timeout.
				window := int64(p.shelveTimeout(ctx).Seconds())
				if d, ok := asSeconds(doc["duration"]); ok && d > 0 {
					window = d
				}
				patch["shelve_until"] = now + window
			case "unshelve":
				// Explicitly lifting the timed shelve clears the deadline.
				patch["shelve_until"] = int64(0)
			}
		}
		rec, err := p.host.DB().GetOne(ctx, "record", db.Document{"uid": uid})
		if err != nil {
			return fmt.Errorf("comment: lookup record %s: %w", uid, err)
		}
		// Escalation bookkeeping, read-modify-written from the row we just
		// fetched. An operator escalating from the UI or a chat command is a
		// first-class escalation producer alongside the escalate-timeout sweep,
		// so the outputs must be able to tell it apart from a first delivery
		// and update the ticket / thread they already created.
		switch commentType {
		case "esc":
			patch["escalation_count"] = int(docInt64(rec, "escalation_count")) + 1
			patch["escalated_at"] = now
			patch["escalation_reason"] = "manual"
			if user, _ := doc["user"].(string); user != "" {
				patch["escalation_actor"] = user
			}
		case "close":
			// Terminal: the next occurrence of this alert is a genuinely new
			// incident, so it must start from a first delivery rather than
			// inheriting the previous lifecycle's escalation count (which would
			// make a notifier comment on a ticket that has been resolved).
			patch["escalation_count"] = 0
			patch["escalated_at"] = int64(0)
			patch["escalation_reason"] = ""
			patch["escalation_actor"] = ""
		}
		reopened := p.applyOwnership(ctx, patch, commentType, doc, rec, now)
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
		// state + ack_until/escalate_at clearing for this transition. A release
		// that returned an acknowledged record to open counts as an open here.
		if commentType == "open" || commentType == "close" || reopened {
			if _, err := p.host.DB().UnsetFields(ctx, "record", []string{"acked_by"},
				condition.Equals("uid", uid)); err != nil {
				return fmt.Errorf("comment: unset acked_by on record %s: %w", uid, err)
			}
		}
		// An operator escalating from the UI or a chat command must reach the
		// output plugins — otherwise "escalate" silently changes a state field
		// and nobody gets paged. Only `esc` re-notifies: ack/open/close/shelve
		// deliberately do not, so notification volume is unchanged for every
		// other transition.
		if commentType == "esc" {
			p.renotify(ctx, uid)
		}
		// A close is not a page, so the dispatcher never re-sends it; notifiers
		// that opened a ticket for this alert hear about it here instead
		// (plugins.CloseNotifier). Detached and best-effort.
		if commentType == "close" {
			user, _ := doc["user"].(string)
			channel, _ := doc["method"].(string)
			plugins.DispatchClose(ctx, p.host, uid, plugins.CloseEvent{
				Actor: user, Channel: channel, At: time.Unix(now, 0).UTC(),
			})
		}
	}
	return nil
}

// applyOwnership folds the ownership change a comment implies into patch,
// computed from rec (the record as stored before this comment). It reports
// whether a `release` moved an acknowledged record back to `open`, which the
// caller must follow with the same acked_by unset as a manual `open`.
//
// The owner is the comment's `user` — server-authoritative once TransformWrite
// has run — but the owner's method comes from the caller's JWT claims, not the
// comment's `method`: the chat-ops bridges overwrite that with the channel
// ("teams", "mcp", …), which is no auth method an avatar could be found under.
// With no claims (a chat surface that authenticates by its own signature) the
// method is "". A user-less ack/close — an auto-comment that bypassed
// TransformWrite — takes nothing, mirroring acked_by.
func (p *Plugin) applyOwnership(ctx context.Context, patch db.Document, commentType string, doc map[string]any, rec db.Document, now int64) (reopened bool) {
	var change map[string]any
	switch commentType {
	case "ack", "close":
		if user, _ := doc["user"].(string); user != "" {
			method := ""
			if claims, ok := auth.ClaimsFrom(ctx); ok {
				// The owner is the key's owner, not the "apikey" channel.
				method = auth.IdentityMethod(claims)
			}
			change = ownership.Take(user, method, now)
		}
	case "open", "esc":
		change = ownership.Clear(rec)
	case "assign":
		// GuardWrite has validated the assignee and filled its method in.
		if assignee, _ := doc["assignee"].(string); assignee != "" {
			method, _ := doc["assignee_method"].(string)
			change = ownership.Take(assignee, method, now)
		}
	case "release":
		_, escalateAfter := p.lifecycleTimeouts(ctx)
		change, reopened = ownership.Release(rec, now, escalateAfter)
	}
	for k, v := range change {
		patch[k] = v
	}
	return reopened
}

// renotify re-fires the notification dispatcher for a manually escalated
// record, mirroring the escalate-timeout sweep's `notify` callback
// (internal/core/boot.go). The record is re-read so the dispatcher sees the
// escalation stamp AfterCreate just wrote.
//
// Best-effort by design: a notifier failure must never fail the comment write,
// which has already landed and is what the operator's action was about. The
// dispatcher's Process only skips ack/close, so an `esc` record dispatches.
func (p *Plugin) renotify(ctx context.Context, uid string) {
	if p.host == nil || p.host.DB() == nil {
		return
	}
	proc, ok := p.host.Plugin("notification").(plugins.Processor)
	if !ok {
		return
	}
	doc, err := p.host.DB().GetOne(ctx, "record", db.Document{"uid": uid})
	if err != nil {
		p.warn("comment: re-notify: record lookup failed", "uid", uid, "error", err)
		return
	}
	rec, err := snoozetypes.RecordFromDocument(doc)
	if err != nil {
		p.warn("comment: re-notify: record decode failed", "uid", uid, "error", err)
		return
	}
	if _, err := proc.Process(ctx, rec); err != nil {
		p.warn("comment: re-notify failed", "uid", uid, "error", err)
	}
}

// warn logs at warn level when the host exposes a logger.
func (p *Plugin) warn(msg string, args ...any) {
	if p.host == nil {
		return
	}
	if lg := p.host.Logger(); lg != nil {
		lg.Warn(msg, args...)
	}
}

// now returns the current time from the injected clock, defaulting to time.Now
// when the plugin was constructed without PostInit (defensive).
func (p *Plugin) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// asSeconds reads a client-supplied second count out of a comment document,
// tolerating every numeric shape a JSON body or a DB driver can produce
// (encoding/json decodes numbers into float64; the Mongo and SQLite drivers
// hand back int/int32/int64). The bool reports whether the value was numeric at
// all, so a non-numeric `duration` is rejected by Validate instead of being
// silently read as zero. A nil value (field absent) is "not numeric".
func asSeconds(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

// docInt64 reads a counter out of a record document, tolerating the int /
// int64 / float64 shapes the Mongo and SQLite drivers decode numbers into.
func docInt64(doc db.Document, key string) int64 {
	switch v := doc[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
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

// resolutionHold resolves the live housekeeping.resolution_hold window a human
// close arms, with the same preference order as lifecycleTimeouts: runtime
// settings, then the file-config baseline, then the schema default. A zero from
// either tier is honoured — it disables the hold.
func (p *Plugin) resolutionHold(ctx context.Context) time.Duration {
	if p.host == nil {
		return schema.DefaultResolutionHold
	}
	if rsh, ok := p.host.(plugins.RuntimeSettingsHost); ok {
		if rs := rsh.RuntimeSettings(); rs != nil {
			return rs.ResolutionHold(ctx)
		}
	}
	if cfg := p.host.Config(); cfg != nil {
		return cfg.Housekeeper.ResolutionHold.AsDuration()
	}
	return schema.DefaultResolutionHold
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
