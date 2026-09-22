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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/protected"
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

// uidResolveTimeoutNoPersist bounds the "wait for the record to land" poll when
// persist_action_outcomes is off. With persist on, the write-back loop has
// already confirmed the row and the resolving read hits on the first try, so it
// can share the full notifierWriteBackTimeout budget. With persist off nothing
// preceded it, and an alert whose record never lands (dropped downstream, DB
// wedged) would otherwise hold the coordinator goroutine for ten seconds per
// dispatch. Two seconds is far more than the pipeline's own write needs.
const uidResolveTimeoutNoPersist = 2 * time.Second

// misconfiguredRowWindow rate-limits the delivery rows a misconfigured action
// produces (D11). The row exists so an admin sees "this never sends, action X
// is missing" — one row every ten minutes says that just as well as one row per
// matching alert, and a broken action on a noisy condition would otherwise
// write a row per ingested alert, forever, on the ingest path.
const misconfiguredRowWindow = 10 * time.Minute

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
//
// NotificationUID and Notifier are NOT part of the stored `actions[]` shape:
// they exist only so the coordinator can attribute a delivery-history row and
// bump the notification's counters without re-resolving the action. Adding
// them to actionResultsToAny would change the record's OpenAPI shape.
type actionResult struct {
	Name            string
	Notification    string
	NotificationUID string
	Notifier        string
	Status          string
	Error           string
}

// sendTask binds a queued notifier send to the result slot it resolves, plus
// everything the delivery-history row for that send needs: the alert snapshot
// (built on the request goroutine so it carries the request tenant and the
// dispatch time) and the per-send external-handle capture.
type sendTask struct {
	idx      int
	notifier plugins.Notifier
	payload  plugins.NotificationPayload
	member   plugins.DeliveryMember
	ref      *refCapture
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

	// writeBackBudget overrides notifierWriteBackTimeout. Zero (the production
	// value) means "use the constant"; tests shrink it so the "the record
	// never lands" path can be exercised without waiting ten seconds for it.
	writeBackBudget time.Duration

	// misconfMu guards misconfSeen, the rate limiter for misconfigured-action
	// delivery rows: key = tenant\x00notification-uid\x00action-name, value =
	// when that combination last produced a row. See recordMisconfigured.
	misconfMu   sync.Mutex
	misconfSeen map[string]time.Time
}

// budget returns the deadline the coordinator gives the action-outcome
// write-back and, separately, the delivery phase.
func (p *Plugin) budget() time.Duration {
	if p.writeBackBudget > 0 {
		return p.writeBackBudget
	}
	return notifierWriteBackTimeout
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

// counterFields are the dispatcher-owned fields on a notification entry. They
// are `readOnly: true` in the OpenAPI schema, which documents intent but
// enforces nothing on the server, so TransformWrite strips them.
var counterFields = []string{"hits", "last_sent"}

// TransformWrite implements plugins.WriteTransformer: it drops the counters a
// client tried to set from every create / replace / patch body.
//
// `hits` and `last_sent` belong to the dispatcher — it advances them from the
// coordinator and from batch flushes, off the request path. Letting a client
// write them would not just fake a number: the editor round-trips whatever the
// API returns, so one PUT of a stale form would silently rewind a live counter.
// Stripping (rather than rejecting) is what the OpenAPI already promises,
// "ignored on write", and keeps a full-object PUT from the UI working.
//
// Nothing else on the entry is dispatcher-owned, so no other field is touched;
// PATCH bodies are partial and a missing key is simply not there to delete.
func (p *Plugin) TransformWrite(_ context.Context, doc map[string]any) error {
	for _, f := range counterFields {
		delete(doc, f)
	}
	return nil
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
	// Misconfigured-action rows are collected here and written from a detached
	// goroutine (D11: nothing about the delivery log runs on the ingest path).
	var misconfigured []plugins.DeliveryRow

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
			r := actionResult{Name: name, Notification: e.Name, NotificationUID: e.UID}
			if skip {
				// D7: a frequency-suppressed action is a config state, not a
				// delivery. Logging it would add one noise row per matching
				// alert, so nothing is written to the history.
				r.Status = actionSkipped
				results = append(results, r)
				continue
			}
			res := p.resolveNotifier(ctx, e, name, rec)
			r.Notifier = res.selected
			if !res.ok {
				r.Status = actionError
				r.Error = res.reason
				results = append(results, r)
				if row, ok := p.misconfiguredRow(ctx, e, rec, name, res); ok {
					misconfigured = append(misconfigured, row)
				}
				continue
			}
			if persist {
				r.Status = actionPending
			} else {
				r.Status = actionSent
			}
			idx := len(results)
			results = append(results, r)
			sends = append(sends, sendTask{
				idx:      idx,
				notifier: res.notifier,
				payload:  res.payload,
				member:   plugins.MemberFromRecord(ctx, rec, e.Name, e.UID),
				ref:      res.ref,
			})
		}
	}

	if rec.Extra == nil && (len(matched) > 0 || len(results) > 0) {
		rec.Extra = map[string]any{}
	}
	if len(matched) > 0 {
		rec.Extra["notifications"] = matched
		// Attribute the dispatch to the first matching notification. Companion
		// daemons read this to say which rule paged the operator in the ticket
		// comment / chat reply they post (see internal/components/jira), and it
		// is what Snooze 1.x stamped. `message` has no source in the Go port —
		// notification entries carry no message field — so it stays absent
		// rather than being faked.
		rec.Extra["notification_from"] = map[string]any{"name": matched[0]}
	}
	if len(results) > 0 {
		rec.Extra["actions"] = actionResultsToAny(results)
	}

	switch {
	case len(sends) > 0:
		// The misconfigured rows ride the coordinator's delivery context, so
		// they share this record's uid resolution and its single write budget.
		p.spawnCoordinator(ctx, rec, results, sends, misconfigured, persist)
	case len(misconfigured) > 0:
		// Nothing resolved to a notifier, so there is no coordinator — but the
		// rows still must not be written on the ingest goroutine.
		p.spawnDeliveryWriter(ctx, misconfigured)
	}

	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// resolution is the outcome of resolving one named action to a live notifier.
// `selected` is the action document's notifier registry key and is set even on
// a failed resolution when the action document itself was found — the
// misconfigured-delivery row records it so an operator sees which notifier the
// action pointed at.
type resolution struct {
	notifier plugins.Notifier
	payload  plugins.NotificationPayload
	ref      *refCapture
	selected string
	ok       bool
	reason   string
}

// resolveNotifier looks up the named action, validates its notifier, and builds
// the send payload. On any miss it returns ok=false plus a human-readable reason
// recorded as the action's error.
func (p *Plugin) resolveNotifier(ctx context.Context, e Entry, name string, rec snoozetypes.Record) resolution {
	ad, ok := p.lookupAction(ctx, name)
	if !ok {
		return resolution{reason: fmt.Sprintf("action %q not found", name)}
	}
	if ad.Action.Selected == "" {
		return resolution{reason: fmt.Sprintf("action %q has no notifier (action.selected empty)", name)}
	}
	plug := p.host.Plugin(ad.Action.Selected)
	if plug == nil {
		return resolution{selected: ad.Action.Selected, reason: fmt.Sprintf("notifier %q not registered", ad.Action.Selected)}
	}
	notifier, ok := plug.(plugins.Notifier)
	if !ok {
		return resolution{selected: ad.Action.Selected, reason: fmt.Sprintf("target %q is not a notifier", ad.Action.Selected)}
	}
	// The capture wraps (never replaces) the record-writing inject closure, so
	// what a notifier stamps onto the record is unchanged; the wrapper only
	// also keeps the external handle in memory for this send's history row.
	ref := &refCapture{browseBase: subcontentString(ad.Action.Subcontent, "jira_url")}
	payload := plugins.NotificationPayload{
		Template:        ad.Action.Selected,
		Meta:            metaFromSubcontent(ad.Action.Subcontent, e, ad.Name),
		Inject:          ref.wrap(p.injectFunc(ctx, rec), rec.Hash),
		Escalation:      plugins.EscalationFrom(rec),
		NotificationUID: e.UID,
	}
	return resolution{notifier: notifier, payload: payload, ref: ref, selected: ad.Action.Selected, ok: true}
}

// misconfiguredRow builds the delivery-history row a misconfigured action
// produces (D7): an admin opening a notification's history must see "this never
// sends, action X is missing" rather than an empty list.
//
// It returns ok=false when an identical (tenant, notification, action) failure
// already produced a row inside misconfiguredRowWindow. A broken action sits on
// a condition that keeps matching, so without that rate limit a single config
// bug would write one row per ingested alert for as long as nobody fixes it —
// and the row says the same thing every time.
//
// The row itself is written by the caller's detached goroutine, never inline:
// this runs on the ingest path.
func (p *Plugin) misconfiguredRow(ctx context.Context, e Entry, rec snoozetypes.Record, name string, res resolution) (plugins.DeliveryRow, bool) {
	tenantID, _ := auth.TenantFrom(ctx)
	if !p.allowMisconfiguredRow(tenantID, e, name, time.Now()) {
		return plugins.DeliveryRow{}, false
	}
	now := time.Now()
	return plugins.DeliveryRow{
		CompletedAt: now,
		QueuedAt:    now,
		Status:      plugins.DeliveryStatusError,
		Error:       res.reason,
		Action:      name,
		Notifier:    res.selected,
		Members:     []plugins.DeliveryMember{plugins.MemberFromRecord(ctx, rec, e.Name, e.UID)},
	}, true
}

// allowMisconfiguredRow is the per-(tenant, notification, action) rate limiter
// behind misconfiguredRow. It reports whether this combination may produce a
// row now, and records the decision when it may.
//
// The map is bounded by the number of DISTINCT broken combinations seen inside
// the window — not by alert volume — because every accepted insert first sweeps
// the entries older than the window. A deployment with a handful of broken
// actions therefore holds a handful of entries, and one that fixes them drops
// back to none within ten minutes.
func (p *Plugin) allowMisconfiguredRow(tenantID string, e Entry, action string, now time.Time) bool {
	notif := e.UID
	if notif == "" {
		// A notification that has not been assigned a uid yet is still worth
		// distinguishing from its siblings; the name is the only key there is.
		notif = "name:" + e.Name
	}
	key := tenantID + "\x00" + notif + "\x00" + action

	p.misconfMu.Lock()
	defer p.misconfMu.Unlock()
	if p.misconfSeen == nil {
		p.misconfSeen = make(map[string]time.Time)
	}
	if last, ok := p.misconfSeen[key]; ok && now.Sub(last) < misconfiguredRowWindow {
		return false
	}
	for k, t := range p.misconfSeen {
		if now.Sub(t) >= misconfiguredRowWindow {
			delete(p.misconfSeen, k)
		}
	}
	p.misconfSeen[key] = now
	return true
}

// spawnDeliveryWriter persists rows on a detached goroutine. Used for the
// records where every matching action was misconfigured: there is no
// coordinator to piggyback on, but the write still must not happen on the
// ingest goroutine (D11) — one synchronous write per alert per broken action is
// exactly the ingest-path cost the plan rules out.
func (p *Plugin) spawnDeliveryWriter(ctx context.Context, rows []plugins.DeliveryRow) {
	host := p.host
	if len(rows) == 0 || host == nil || host.DB() == nil {
		return
	}
	tenantID, _ := auth.TenantFrom(ctx)
	go func() { //nolint:gosec // detached: the request ctx is cancelled on return
		writeCtx, cancel := context.WithTimeout(context.Background(), p.budget())
		defer cancel()
		if tenantID != "" {
			writeCtx = auth.WithTenant(writeCtx, tenantID)
		}
		for _, row := range rows {
			plugins.RecordDelivery(writeCtx, host, row)
		}
	}()
}

// subcontentString reads a string out of an action's subcontent map.
func subcontentString(sub map[string]any, key string) string {
	if sub == nil {
		return ""
	}
	s, _ := sub[key].(string)
	return s
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
// resolves each pending result to success/error, then writes the fully-resolved
// actions array back to the record (when persist is true) via a hash-keyed
// SetFields (uid fallback), resolves the record uid for the delivery rows, bumps
// the notification counters and persists the delivery history (including any
// misconfigured-action rows this record produced).
//
// Two separate write contexts, deliberately: the hash-keyed write-back polls
// until the pipeline's own write of the record lands and is allowed to burn its
// entire budget doing so. Sharing that context with the delivery work meant a
// record that never landed left every row to be written on an already-dead
// context — the write silently no-oped and the history lost the send. The
// delivery phase therefore starts from a fresh budget.
//
// The delivery writes are deliberately NOT gated by persist: turning the
// action-outcome stamp off is a record-shape choice, while the delivery log has
// its own switch (notification.delivery_log).
func (p *Plugin) spawnCoordinator(ctx context.Context, rec snoozetypes.Record, results []actionResult, sends []sendTask, misconfigured []plugins.DeliveryRow, persist bool) {
	host := p.host
	eventEpoch := rec.DateEpoch
	hash := rec.Hash
	uid := rec.UID
	tenantID, _ := auth.TenantFrom(ctx)
	loopChain := auth.LoopChainFrom(ctx)
	// Dispatch time, captured once on the request goroutine: every row from
	// this record shares it as `queued_epoch`.
	queuedAt := time.Now()

	go func() { //nolint:gosec // detached: the request ctx is cancelled on return
		var wg sync.WaitGroup
		var mu sync.Mutex
		rows := make([]plugins.DeliveryRow, 0, len(sends))
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
				start := time.Now()
				sendErr := st.notifier.Send(sendCtx, rec, st.payload)
				if errors.Is(sendErr, plugins.ErrBatched) {
					// D8: the notifier accepted the alert into a batch bucket.
					// The outcome belongs to the flush, which writes the row and
					// owns the success/error accounting — so no stat, no row, no
					// warning here, and the record's action reads "sent".
					mu.Lock()
					results[st.idx].Status = actionSent
					mu.Unlock()
					if lg := p.logger(); lg != nil {
						lg.Debug("notification: send queued for batch delivery",
							"action", results[st.idx].Name,
							"notification", results[st.idx].Notification,
							"selected", st.payload.Template)
					}
					return
				}
				status, metric := actionSuccess, "action_success"
				deliveryStatus := plugins.DeliveryStatusSuccess
				if sendErr != nil {
					status, metric = actionError, "action_error"
					deliveryStatus = plugins.DeliveryStatusError
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
				row := plugins.DeliveryRow{
					CompletedAt: time.Now(),
					QueuedAt:    queuedAt,
					Duration:    time.Since(start),
					Status:      deliveryStatus,
					Action:      results[st.idx].Name,
					Notifier:    results[st.idx].Notifier,
					Members:     []plugins.DeliveryMember{st.member},
					Ref:         st.ref.snapshot(),
				}
				if sendErr != nil {
					row.Error = sendErr.Error()
				}
				mu.Lock()
				results[st.idx].Status = status
				if sendErr != nil {
					results[st.idx].Error = sendErr.Error()
				}
				rows = append(rows, row)
				mu.Unlock()
			}(st)
		}
		wg.Wait()

		if host == nil || host.DB() == nil {
			return
		}

		// landed reports whether the write-back actually found the record. It
		// is the answer to "is there a row to read a uid off?", so when the
		// write-back gave up there is no point polling for the same row again.
		landed := false
		if persist {
			writeCtx, cancel := context.WithTimeout(context.Background(), p.budget())
			if tenantID != "" {
				writeCtx = auth.WithTenant(writeCtx, tenantID)
			}
			landed = p.writeBackOutcomes(writeCtx, results, hash, uid)
			cancel()
		}

		rows = append(rows, misconfigured...)
		if len(rows) == 0 {
			return
		}

		// A fresh budget for the delivery phase: whatever the write-back spent
		// waiting for the record is its own business.
		deliverCtx, cancelDeliver := context.WithTimeout(context.Background(), p.budget())
		defer cancelDeliver()
		if tenantID != "" {
			deliverCtx = auth.WithTenant(deliverCtx, tenantID)
		}

		// Evaluated once, and only the log-row work hangs off it: an operator
		// who turned the delivery log off asked for no history, not for slower
		// dispatch, so with it off this goroutine does no uid read and no row
		// write at all. The counters are a separate feature with no switch.
		logEnabled := plugins.DeliveryLogEnabled(deliverCtx, host)

		// D9: a first-occurrence record has no uid inside Process (the driver
		// mints it in the pipeline's final write, which runs after Process
		// returns). Resolve it once for every row of this record; a row that
		// cannot get one keeps its hash and the UI links by hash instead.
		if logEnabled && uid == "" && hash != "" && (landed || !persist) {
			uidCtx := deliverCtx
			if !persist {
				// No write-back loop preceded this, so the poll below is the
				// thing waiting for the record to land. Give it a short leash.
				var cancelUID context.CancelFunc
				uidCtx, cancelUID = context.WithTimeout(deliverCtx, uidResolveTimeoutNoPersist)
				defer cancelUID()
			}
			if resolved := p.resolveRecordUID(uidCtx, hash); resolved != "" {
				for i := range rows {
					for j := range rows[i].Members {
						if rows[i].Members[j].UID == "" {
							rows[i].Members[j].UID = resolved
						}
					}
				}
			}
		}

		plugins.BumpNotificationCounters(deliverCtx, host, deliveredMembers(rows))
		if !logEnabled {
			return
		}
		for _, row := range rows {
			plugins.RecordDelivery(deliverCtx, host, row)
		}
	}()
}

// deliveredMembers flattens the members of the SUCCESSFUL rows, which are the
// only ones allowed to move a notification's counters (D10). The per-
// notification de-duplication happens in plugins.BumpNotificationCounters, so
// two actions of the same notification still count as one hit.
func deliveredMembers(rows []plugins.DeliveryRow) []plugins.DeliveryMember {
	var out []plugins.DeliveryMember
	for _, row := range rows {
		if row.Status != plugins.DeliveryStatusSuccess {
			continue
		}
		out = append(out, row.Members...)
	}
	return out
}

// writeBackOutcomes persists the fully-resolved actions array onto the record,
// keyed by hash (with a uid fallback). Hash-keyed writes retry until the
// pipeline's own write of the record lands or writeCtx expires — see
// writeBackRetryInterval.
//
// It reports whether a stored record was actually matched. The caller uses that
// as "there is a row to read a uid off": when the write-back gave up because
// the record never appeared, re-polling for the same row would just burn the
// delivery budget on a second doomed wait.
func (p *Plugin) writeBackOutcomes(writeCtx context.Context, results []actionResult, hash, uid string) bool {
	host := p.host
	patch := db.Document{"actions": actionResultsToAny(results)}
	if hash != "" {
		cond := condition.Equals("hash", hash)
		for {
			matched, err := host.DB().SetFields(writeCtx, recordCollectionName, patch, cond)
			if err != nil {
				if lg := p.logger(); lg != nil {
					lg.Warn("notification: action-outcome write-back (hash) failed", "hash", hash, "err", err)
				}
				return false
			}
			if matched > 0 {
				return true
			}
			// No row yet: the caller (e.g. the pipeline's final writeRecord)
			// may not have persisted the record. Retry until it lands or the
			// write-back deadline expires.
			select {
			case <-writeCtx.Done():
				if lg := p.logger(); lg != nil {
					lg.Warn("notification: action-outcome write-back (hash) gave up: record never appeared", "hash", hash)
				}
				return false
			case <-time.After(writeBackRetryInterval):
			}
		}
	}
	if uid != "" {
		if err := host.DB().UpdateOne(writeCtx, recordCollectionName, uid, patch, false); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: action-outcome write-back (uid) failed", "uid", uid, "err", err)
			}
			return false
		}
		return true
	}
	if lg := p.logger(); lg != nil {
		lg.Warn("notification: action-outcome write-back skipped: record has neither hash nor uid")
	}
	return false
}

// resolveRecordUID reads the stored record's uid by hash so a delivery row can
// carry the alert's stable key.
//
// It retries on a miss for the same reason writeBackOutcomes does: the record
// may not have been persisted yet. When persist is on, the write-back has
// usually already confirmed the row and the first read hits; when it is off (no
// write-back loop) this bounded retry is what waits for the record. Returns ""
// on any failure — the row still carries the hash, which is enough for the UI.
func (p *Plugin) resolveRecordUID(ctx context.Context, hash string) string {
	for {
		doc, err := p.host.DB().GetOne(ctx, recordCollectionName, db.Document{"hash": hash})
		if err == nil {
			s, _ := doc["uid"].(string)
			return s
		}
		if !errors.Is(err, db.ErrNotFound) {
			return ""
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(writeBackRetryInterval):
		}
	}
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
		// Every notifier in-tree derives this name from a fixed prefix
		// (`response_<action>`, `notify_ref_<action>`), so a protected field
		// is not reachable today. The check is here because this closure is
		// the single chokepoint for an extension point: a notifier is free to
		// call plugins.InjectField with any name it likes, and that must not
		// become the way around the protected-field endpoint.
		if protected.IsProtected(field) {
			if lg := p.logger(); lg != nil {
				lg.Warn("notification: refused an inject onto a protected field", "field", field)
			}
			return
		}
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

// refCapture keeps the external handle a notifier stores during ONE send
// (jira's issue key, slack's thread ts, telegram's message id …) so the
// delivery-history row can carry it and the UI can offer an "Open OPS-123 ↗"
// link.
//
// It observes rather than intercepts: wrap() returns a closure that records the
// handle and then calls the real inject closure unchanged, so what a notifier
// writes onto the record is exactly what it wrote before. The mutex is not
// decorative — Inject runs on the notifier's send goroutine while the
// coordinator reads the snapshot from another.
type refCapture struct {
	// browseBase is the action's `jira_url`, used to derive a browse URL from
	// an issue key. Empty for every other notifier.
	browseBase string

	mu  sync.Mutex
	ref map[string]any
}

// wrap decorates next with the handle capture. next may be nil (no DB handle,
// or a record with neither hash nor uid) — the capture still works and the
// record write stays skipped, exactly as before.
func (c *refCapture) wrap(next plugins.InjectFunc, hash string) plugins.InjectFunc {
	return func(field string, value any) {
		c.observe(field, value, hash)
		if next != nil {
			next(field, value)
		}
	}
}

// observe keeps the value of a notify_ref_<action> / response_<action> field.
// The closure is per (send, action), so every handle field reaching it belongs
// to this send; anything else (a notifier stamping an unrelated field) is
// ignored.
//
// A batch-shaped handle — `{"<hash>": {...}}`, which receivers answering per
// alert produce even for a single alert — is unwrapped to this record's entry,
// mirroring plugins.NotifyRef.
func (c *refCapture) observe(field string, value any, hash string) {
	if !plugins.IsNotifyRefField(field) {
		return
	}
	ref, ok := value.(map[string]any)
	if !ok || len(ref) == 0 {
		return
	}
	if hash != "" {
		if inner, nested := ref[hash].(map[string]any); nested && len(inner) > 0 {
			ref = inner
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ref == nil {
		c.ref = make(map[string]any, len(ref)+1)
	}
	for k, v := range ref {
		c.ref[k] = v
	}
}

// snapshot returns a copy of the captured handle, with a `url` added when one
// is derivable, or nil when the notifier stored nothing. Nil-receiver safe so
// call sites never need to guard.
func (c *refCapture) snapshot() map[string]any {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ref) == 0 {
		return nil
	}
	out := make(map[string]any, len(c.ref)+1)
	for k, v := range c.ref {
		out[k] = v
	}
	if _, has := out["url"]; !has && c.browseBase != "" {
		if key, _ := out["issue_key"].(string); key != "" {
			out["url"] = strings.TrimRight(c.browseBase, "/") + "/browse/" + key
		}
	}
	return out
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
