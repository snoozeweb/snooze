// Package notification implements the `notification` core Processor plugin.
//
// A notification entry pairs a Condition with a set of named action targets.
// When a record matches the entry's condition (and falls inside the entry's
// time-constraint window), the plugin resolves each named action against the
// `action` collection, picks the Notifier plugin named by `action.selected`,
// and invokes Send synchronously on a fresh goroutine. This mirrors the
// Python 1.x behaviour (Notification.send → ActionObject.send → action_plugin.send)
// without the bus indirection the v2.0 rewrite scaffolded but never wired.
//
// # Action cache
//
// Action documents are cached in-memory and refreshed on every Reload of
// either the notification or the action collection. A miss on dispatch
// triggers a lazy refresh once so a freshly-created action becomes
// dispatchable without waiting for the next sync event.
package notification

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/timeconstraints"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

// collectionName is the database collection the plugin owns.
const collectionName = "notification"

// actionCollectionName is the sibling collection the dispatcher reads on every
// match to discover (selected, subcontent) for each named action.
const actionCollectionName = "action"

// recordCollectionName is the alert/record collection. Used by the
// inject_response path to UpdateOne the record with a notifier-supplied
// field (e.g. webhook's parsed HTTP response).
const recordCollectionName = "record"

// notifierSendTimeout caps a single Notifier.Send call. The pipeline goroutine
// returns immediately, but the send goroutine itself enforces this deadline so
// a hung HTTP target cannot leak goroutines indefinitely.
const notifierSendTimeout = 30 * time.Second

// Action is one entry in a notification's `actions` array on the wire. The
// Python code only stores the action name (a string) — this Go port accepts
// the same shape via UnmarshalJSON.
type actionRef = string

// Entry mirrors one row in the `notification` collection. Field names match
// the wire shape used by the Python backend.
type Entry struct {
	UID             string                `json:"uid,omitempty"`
	Name            string                `json:"name"`
	Enabled         *bool                 `json:"enabled,omitempty"`
	Condition       condition.Cond        `json:"condition,omitempty"`
	TimeConstraints timeconstraints.Group `json:"time_constraints,omitempty"`
	Actions         []actionRef           `json:"actions,omitempty"`
	Frequency       *Frequency            `json:"frequency,omitempty"`
}

// IsEnabled reports the effective enabled flag (default true if unset).
func (e Entry) IsEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// Frequency carries the throttle parameters for repeated delivery. The values
// are forwarded to the Notifier plugin via NotificationPayload.Meta so future
// delay/retry logic can reuse them. The dispatcher itself only honours
// total == 0 as "skip" today.
type Frequency struct {
	Total int `json:"total,omitempty"`
	Delay int `json:"delay,omitempty"`
	Every int `json:"every,omitempty"`
}

// actionDoc is the decoded view of one row in the `action` collection.
type actionDoc struct {
	Name   string         `json:"name"`
	Action actionEnvelope `json:"action"`
}

// actionEnvelope mirrors the {selected, subcontent} pair stored at
// `action.action` (yes, the column nests under the same name in Python).
type actionEnvelope struct {
	Selected   string         `json:"selected"`
	Subcontent map[string]any `json:"subcontent"`
}

// Plugin is the notification dispatcher.
//
// Lifecycle: Register → factory → PostInit (loads entries + actions from the
// DB) → Process (per record) → Reload (re-reads both collections on collection
// change).
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	mu      sync.RWMutex
	entries []Entry
	actions map[string]actionDoc
}

// Name returns the registry key. Returned lowercase so it matches the
// HTTP path segment in api/openapi.yaml's PluginPath enum
// (metadata.yaml's `name:` is human-readable and may be capitalised).
func (p *Plugin) Name() string { return "notification" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in and performs the initial entry+action load.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.host = host
	return p.Reload(ctx)
}

// ReloadCollections declares the `action` collection as a reload dependency
// (the syncer's ReloadDeps interface). The dispatcher caches action documents
// in memory but owns only the `notification` collection, so without this an
// edit to an action (url/payload/inject_response/…) would not reach the running
// dispatcher until a restart, a notification-collection change, or a cache miss
// on a brand-new action name. Declaring the dependency makes action edits take
// effect immediately, cluster-wide.
func (p *Plugin) ReloadCollections() []string {
	return []string{actionCollectionName}
}

// Reload re-reads both the notification and action collections. Failures on
// either surface to the syncer; the existing caches are kept on transient
// errors so the pipeline keeps dispatching the last-known-good state.
func (p *Plugin) Reload(ctx context.Context) error {
	if p.host == nil || p.host.DB() == nil {
		return nil
	}
	entries, err := p.loadEntries(ctx)
	if err != nil {
		return err
	}
	actions, err := p.loadActions(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.entries = entries
	p.actions = actions
	p.mu.Unlock()
	return nil
}

func (p *Plugin) loadEntries(ctx context.Context) ([]Entry, error) {
	docs, _, err := p.host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{})
	if err != nil {
		return nil, fmt.Errorf("notification: load entries: %w", err)
	}
	entries := make([]Entry, 0, len(docs))
	for _, d := range docs {
		e, ok := decodeEntry(d)
		if !ok {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: skipping malformed entry", "doc", d)
			}
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (p *Plugin) loadActions(ctx context.Context) (map[string]actionDoc, error) {
	docs, _, err := p.host.DB().Search(ctx, actionCollectionName, condition.Cond{}, db.Page{})
	if err != nil {
		return nil, fmt.Errorf("notification: load actions: %w", err)
	}
	out := make(map[string]actionDoc, len(docs))
	for _, d := range docs {
		ad, ok := decodeActionDoc(d)
		if !ok {
			continue
		}
		out[ad.Name] = ad
	}
	return out, nil
}

// decodeActionDoc round-trips a free-form action document through JSON into
// the typed actionDoc shape. Documents missing a name or a selected notifier
// are silently dropped — the dispatcher logs the miss at use time.
func decodeActionDoc(d db.Document) (actionDoc, bool) {
	if d == nil {
		return actionDoc{}, false
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return actionDoc{}, false
	}
	var ad actionDoc
	if err := json.Unmarshal(raw, &ad); err != nil {
		return actionDoc{}, false
	}
	if ad.Name == "" {
		return actionDoc{}, false
	}
	return ad, true
}

// Process inspects rec and dispatches one Notifier.Send per matching action.
// The verdict is always ActionContinue: notification is a side-effect, not a
// pipeline gate. Records in the ack/close states are skipped to match Python.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	if rec.State == "ack" || rec.State == "close" {
		return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
	}

	p.mu.RLock()
	entries := p.entries
	p.mu.RUnlock()
	if len(entries) == 0 {
		return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
	}

	recMap := recordToMap(rec)
	now := recordTime(rec)

	for _, e := range entries {
		if !e.IsEnabled() {
			continue
		}
		if !condition.Match(recMap, e.Condition) {
			continue
		}
		if !e.TimeConstraints.Match(now) {
			continue
		}
		p.dispatch(ctx, e, rec)
	}

	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// dispatch resolves each action name against the cached action map and fires
// the matching Notifier.Send on a detached goroutine. Errors are logged but
// never propagate back to the pipeline — delivery is best-effort by design.
func (p *Plugin) dispatch(ctx context.Context, e Entry, rec snoozetypes.Record) {
	if len(e.Actions) == 0 {
		return
	}
	if e.Frequency != nil && e.Frequency.Total == 0 {
		// Python: action_obj['total'] == 0 means "do not send"; we honour
		// the same convention so operators can stage a notification before
		// flipping it on.
		return
	}

	for _, name := range e.Actions {
		ad, ok := p.lookupAction(ctx, name)
		if !ok {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: action not found",
					"notification", e.Name,
					"action", name)
			}
			continue
		}
		if ad.Action.Selected == "" {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: action missing notifier (action.selected empty)",
					"notification", e.Name,
					"action", name)
			}
			continue
		}
		plug := p.host.Plugin(ad.Action.Selected)
		if plug == nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: notifier plugin not registered",
					"notification", e.Name,
					"action", name,
					"selected", ad.Action.Selected)
			}
			continue
		}
		notifier, ok := plug.(plugins.Notifier)
		if !ok {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: target plugin is not a Notifier",
					"notification", e.Name,
					"action", name,
					"selected", ad.Action.Selected)
			}
			continue
		}
		payload := plugins.NotificationPayload{
			Template: ad.Action.Selected,
			Meta:     metaFromSubcontent(ad.Action.Subcontent, e, ad.Name),
			Inject:   p.injectFunc(rec),
		}
		p.fireSend(notifier, rec, payload, e.Name, name)
	}
}

// lookupAction returns the cached action doc, refreshing the cache once on a
// miss so freshly-created actions become dispatchable without waiting for the
// next sync event.
func (p *Plugin) lookupAction(ctx context.Context, name string) (actionDoc, bool) {
	p.mu.RLock()
	ad, ok := p.actions[name]
	p.mu.RUnlock()
	if ok {
		return ad, true
	}
	if p.host == nil || p.host.DB() == nil {
		return actionDoc{}, false
	}
	fresh, err := p.loadActions(ctx)
	if err != nil {
		return actionDoc{}, false
	}
	p.mu.Lock()
	p.actions = fresh
	p.mu.Unlock()
	ad, ok = fresh[name]
	return ad, ok
}

// metaFromSubcontent shallow-clones the action's subcontent map and stamps
// the action name into it as `action_name` (Python places it there too, so
// downstream notifier code can reference it).
func metaFromSubcontent(sub map[string]any, e Entry, actionName string) map[string]any {
	out := make(map[string]any, len(sub)+2)
	for k, v := range sub {
		out[k] = v
	}
	out["action_name"] = actionName
	out["notification_name"] = e.Name
	if e.Frequency != nil {
		out["frequency"] = map[string]any{
			"total": e.Frequency.Total,
			"delay": e.Frequency.Delay,
			"every": e.Frequency.Every,
		}
	}
	return out
}

// injectFunc returns the closure handed to Notifier.Send via
// NotificationPayload.Inject. Notifiers (today: only webhook with
// `inject_response: true`) call it to stamp `response_<action_name>`-style
// fields back onto the record DB row. The closure is best-effort: an empty
// identity or DB error is logged and swallowed.
//
// The write keys on `hash`, not `uid`, mirroring Snooze 1.x's
// `db.write('record', succeeded, 'hash')`. This matters on a record's FIRST
// fire: aggregaterule only mints a uid when it finds an existing aggregate, so
// the in-memory record dispatched on a first occurrence has no uid yet (the DB
// assigns one in the pipeline's final write). It always has a hash by the time
// the notification plugin runs, so a hash-keyed SetFields lands the response
// even on the first fire. SetFields is a field-targeted merge, so concurrent
// updates from the pipeline aren't clobbered.
//
// Returns nil when there is neither a hash nor a uid to target, or no DB
// handle — all cases short-circuit any inject call to a no-op via
// plugins.InjectField.
func (p *Plugin) injectFunc(rec snoozetypes.Record) plugins.InjectFunc {
	if p.host == nil || p.host.DB() == nil {
		return nil
	}
	hash := rec.Hash
	uid := rec.UID
	if hash == "" && uid == "" {
		return nil
	}
	return func(field string, value any) {
		ctx, cancel := context.WithTimeout(context.Background(), notifierSendTimeout)
		defer cancel()
		patch := db.Document{field: value}
		if hash != "" {
			if _, err := p.host.DB().SetFields(ctx, recordCollectionName, patch, condition.Equals("hash", hash)); err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("notification: inject_response: SetFields by hash failed",
						"hash", hash, "field", field, "err", err)
				}
			}
			return
		}
		// No hash (record never went through aggregaterule) — fall back to the
		// uid-keyed update.
		if err := p.host.DB().UpdateOne(ctx, recordCollectionName, uid, patch, false); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: inject_response: UpdateOne failed",
					"uid", uid, "field", field, "err", err)
			}
		}
	}
}

// fireSend launches a detached goroutine that invokes notifier.Send under a
// fresh background context capped by notifierSendTimeout. The pipeline ctx is
// intentionally not propagated: it is cancelled as soon as Process returns,
// which would prematurely abort every Notifier round-trip.
func (p *Plugin) fireSend(notifier plugins.Notifier, rec snoozetypes.Record, payload plugins.NotificationPayload, notification, action string) {
	go func() {
		sendCtx, cancel := context.WithTimeout(context.Background(), notifierSendTimeout)
		defer cancel()
		if err := notifier.Send(sendCtx, rec, payload); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: notifier send failed",
					"notification", notification,
					"action", action,
					"selected", payload.Template,
					"err", err)
			}
		}
	}()
}

// logger returns the host logger or the default if the host is missing one.
func (p *Plugin) logger() *slog.Logger {
	if p.host == nil {
		return slog.Default()
	}
	return p.host.Logger()
}

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

func init() {
	plugins.Register("notification", metaYAML, factory)
}
