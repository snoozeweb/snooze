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

	"github.com/snoozeweb/snooze/internal/auth"
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

// notifierWriteBackTimeout caps the single action-outcome SetFields/UpdateOne
// write-back. A DB merge is far faster than a notifier round-trip, so it gets a
// tighter bound than notifierSendTimeout to keep the coordinator goroutine short-lived.
const notifierWriteBackTimeout = 10 * time.Second

// writeBackRetryInterval is the pause between hash-keyed write-back attempts.
// The coordinator goroutine can finish (and try to persist the resolved
// status) before the pipeline's own initial write of the record lands, since
// Process spawns the goroutine and returns before its caller persists the
// record (see internal/core/pipeline.go: notification is last in the default
// process order, so the final writeRecord happens immediately after Process
// returns). SetFields is a no-op when no row matches yet, so without a retry
// the resolved status would be silently dropped. Retrying within
// notifierWriteBackTimeout closes that window.
const writeBackRetryInterval = 20 * time.Millisecond

// Action-outcome statuses stamped onto record.actions.
const (
	actionPending = "pending"
	actionSent    = "sent"
	actionSuccess = "success"
	actionError   = "error"
	actionSkipped = "skipped"
)

// actionResult is the in-memory form of one record.actions entry. It is
// converted to a plain map (actionResultsToAny) before being stored so it
// serializes identically across all three DB backends.
type actionResult struct {
	Name         string
	Notification string
	Status       string
	Error        string
}

// sendTask binds a queued notifier send to the result slot it resolves.
type sendTask struct {
	idx      int
	notifier plugins.Notifier
	payload  plugins.NotificationPayload
}

func actionResultsToAny(results []actionResult) []any {
	out := make([]any, len(results))
	for i, r := range results {
		m := map[string]any{"name": r.Name, "status": r.Status}
		if r.Notification != "" {
			m["notification"] = r.Notification
		}
		if r.Error != "" {
			m["error"] = r.Error
		}
		out[i] = m
	}
	return out
}

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
	entries map[string][]Entry              // tenantID → entries
	actions map[string]map[string]actionDoc // tenantID → name → actionDoc
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
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
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
	if p.entries == nil {
		p.entries = make(map[string][]Entry)
	}
	if p.actions == nil {
		p.actions = make(map[string]map[string]actionDoc)
	}
	p.entries[tenantID] = entries
	p.actions[tenantID] = actions
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

	tenantID, _ := auth.TenantFrom(ctx)

	p.mu.RLock()
	entries := p.entries[tenantID]
	p.mu.RUnlock()
	if len(entries) == 0 {
		return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
	}

	recMap := recordToMap(rec)
	now := recordTime(rec)

	persist := p.persistActionOutcomes()

	var matched []string
	var results []actionResult
	var sends []sendTask

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
		matched = append(matched, e.Name)
		if len(e.Actions) == 0 {
			continue
		}
		skip := e.Frequency != nil && e.Frequency.Total == 0
		if !skip {
			plugins.RecordStat(ctx, p.host, rec.DateEpoch, "notification_sent",
				map[string]string{"name": e.Name}, 1)
		}
		for _, name := range e.Actions {
			r := actionResult{Name: name, Notification: e.Name}
			if skip {
				r.Status = actionSkipped
				results = append(results, r)
				continue
			}
			notifier, payload, ok, reason := p.resolveNotifier(ctx, e, name, rec)
			if !ok {
				r.Status = actionError
				r.Error = reason
				results = append(results, r)
				continue
			}
			if persist {
				r.Status = actionPending
			} else {
				r.Status = actionSent
			}
			idx := len(results)
			results = append(results, r)
			sends = append(sends, sendTask{idx: idx, notifier: notifier, payload: payload})
		}
	}

	if rec.Extra == nil && (len(matched) > 0 || len(results) > 0) {
		rec.Extra = map[string]any{}
	}
	if len(matched) > 0 {
		rec.Extra["notifications"] = matched
	}
	if len(results) > 0 {
		rec.Extra["actions"] = actionResultsToAny(results)
	}

	if len(sends) > 0 {
		p.spawnCoordinator(ctx, rec, results, sends, persist)
	}

	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// resolveNotifier looks up the named action, validates its notifier, and builds
// the send payload. On any miss it returns ok=false plus a human-readable reason
// recorded as the action's error.
func (p *Plugin) resolveNotifier(ctx context.Context, e Entry, name string, rec snoozetypes.Record) (plugins.Notifier, plugins.NotificationPayload, bool, string) {
	ad, ok := p.lookupAction(ctx, name)
	if !ok {
		return nil, plugins.NotificationPayload{}, false, fmt.Sprintf("action %q not found", name)
	}
	if ad.Action.Selected == "" {
		return nil, plugins.NotificationPayload{}, false, fmt.Sprintf("action %q has no notifier (action.selected empty)", name)
	}
	plug := p.host.Plugin(ad.Action.Selected)
	if plug == nil {
		return nil, plugins.NotificationPayload{}, false, fmt.Sprintf("notifier %q not registered", ad.Action.Selected)
	}
	notifier, ok := plug.(plugins.Notifier)
	if !ok {
		return nil, plugins.NotificationPayload{}, false, fmt.Sprintf("target %q is not a notifier", ad.Action.Selected)
	}
	payload := plugins.NotificationPayload{
		Template:   ad.Action.Selected,
		Meta:       metaFromSubcontent(ad.Action.Subcontent, e, ad.Name),
		Inject:     p.injectFunc(ctx, rec),
		Escalation: plugins.EscalationFrom(rec),
	}
	return notifier, payload, true, ""
}

// persistActionOutcomes reports whether the async resolution write-back is on.
// Defaults true when no config is wired (matches DefaultNotification).
func (p *Plugin) persistActionOutcomes() bool {
	if p.host == nil {
		return true
	}
	if cfg := p.host.Config(); cfg != nil {
		return cfg.Notification.PersistActionOutcomes
	}
	return true
}

// spawnCoordinator fires every queued send concurrently on a single detached
// goroutine (so Process returns immediately), records the per-send metrics,
// resolves each pending result to success/error, then — when persist is true —
// writes the fully-resolved actions array back to the record exactly once via a
// hash-keyed SetFields (uid fallback), mirroring injectFunc's tenant re-stamp.
func (p *Plugin) spawnCoordinator(ctx context.Context, rec snoozetypes.Record, results []actionResult, sends []sendTask, persist bool) {
	host := p.host
	eventEpoch := rec.DateEpoch
	hash := rec.Hash
	uid := rec.UID
	tenantID, _ := auth.TenantFrom(ctx)
	loopChain := auth.LoopChainFrom(ctx)

	go func() { //nolint:gosec // detached: the request ctx is cancelled on return
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, st := range sends {
			wg.Add(1)
			go func(st sendTask) {
				defer wg.Done()
				sendCtx, cancel := context.WithTimeout(context.Background(), notifierSendTimeout)
				defer cancel()
				if tenantID != "" {
					sendCtx = auth.WithTenant(sendCtx, tenantID)
				}
				if len(loopChain) > 0 {
					sendCtx = auth.WithLoopChain(sendCtx, loopChain)
				}
				sendErr := st.notifier.Send(sendCtx, rec, st.payload)
				status, metric := actionSuccess, "action_success"
				if sendErr != nil {
					status, metric = actionError, "action_error"
					if lg := p.logger(); lg != nil {
						lg.Warn("notification: notifier send failed",
							"action", results[st.idx].Name,
							"notification", results[st.idx].Notification,
							"selected", st.payload.Template,
							"err", sendErr)
					}
				}
				plugins.RecordStat(sendCtx, host, eventEpoch, metric,
					map[string]string{"name": results[st.idx].Name}, 1)
				mu.Lock()
				results[st.idx].Status = status
				if sendErr != nil {
					results[st.idx].Error = sendErr.Error()
				}
				mu.Unlock()
			}(st)
		}
		wg.Wait()

		if !persist || host == nil || host.DB() == nil {
			return
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), notifierWriteBackTimeout)
		defer cancel()
		if tenantID != "" {
			writeCtx = auth.WithTenant(writeCtx, tenantID)
		}
		patch := db.Document{"actions": actionResultsToAny(results)}
		if hash != "" {
			cond := condition.Equals("hash", hash)
			for {
				matched, err := host.DB().SetFields(writeCtx, recordCollectionName, patch, cond)
				if err != nil {
					if lg := p.logger(); lg != nil {
						lg.Warn("notification: action-outcome write-back (hash) failed", "hash", hash, "err", err)
					}
					return
				}
				if matched > 0 {
					return
				}
				// No row yet: the caller (e.g. the pipeline's final writeRecord)
				// may not have persisted the record. Retry until it lands or the
				// write-back deadline expires.
				select {
				case <-writeCtx.Done():
					if lg := p.logger(); lg != nil {
						lg.Warn("notification: action-outcome write-back (hash) gave up: record never appeared", "hash", hash)
					}
					return
				case <-time.After(writeBackRetryInterval):
				}
			}
		}
		if uid != "" {
			if err := host.DB().UpdateOne(writeCtx, recordCollectionName, uid, patch, false); err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("notification: action-outcome write-back (uid) failed", "uid", uid, "err", err)
				}
			}
			return
		}
		if lg := p.logger(); lg != nil {
			lg.Warn("notification: action-outcome write-back skipped: record has neither hash nor uid")
		}
	}()
}

// lookupAction returns the cached action doc, refreshing the cache once on a
// miss so freshly-created actions become dispatchable without waiting for the
// next sync event.
func (p *Plugin) lookupAction(ctx context.Context, name string) (actionDoc, bool) {
	tenantID, _ := auth.TenantFrom(ctx)

	p.mu.RLock()
	ad, ok := p.actions[tenantID][name]
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
	if p.actions == nil {
		p.actions = make(map[string]map[string]actionDoc)
	}
	p.actions[tenantID] = fresh
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
//
// The inject runs on the notifier's detached goroutine, so the closure rebuilds
// a fresh background context (the pipeline ctx is long cancelled by then) and
// re-stamps the tenant captured from the dispatch ctx via auth.WithTenant, so
// the tenant-scoped record write lands in the originating tenant's partition
// instead of fail-closing on a naked context.
func (p *Plugin) injectFunc(ctx context.Context, rec snoozetypes.Record) plugins.InjectFunc {
	if p.host == nil || p.host.DB() == nil {
		return nil
	}
	hash := rec.Hash
	uid := rec.UID
	if hash == "" && uid == "" {
		return nil
	}
	tenantID, _ := auth.TenantFrom(ctx)
	return func(field string, value any) {
		ctx, cancel := context.WithTimeout(context.Background(), notifierSendTimeout)
		defer cancel()
		if tenantID != "" {
			ctx = auth.WithTenant(ctx, tenantID)
		}
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
