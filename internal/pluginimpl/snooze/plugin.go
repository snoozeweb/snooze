// Package snooze implements the namesake "snooze" Processor plugin: a record
// pipeline filter that silences matching alerts during configured time
// windows. The plugin caches its rule set in memory (refreshed on PostInit
// and Reload from the "snooze" collection) and bumps a per-rule hit counter
// on each match.
//
// # Ownership of the `snoozed` attribution
//
// This plugin is the plugins.SuppressionOwner: it is the only code that sets,
// keeps or clears a record's `snoozed` field. Every branch of Process states
// its decision about the field explicitly, and the five together are total:
//
//	close against an existing aggregate → KEEP  (not re-deciding — see below)
//	severity in snooze_bypass_severities → CLEAR (this severity is never silenced)
//	rule matches, discard               → keep  (nothing is persisted at all)
//	rule matches, tag                   → SET   (the matching rule's name)
//	no rule matches                     → CLEAR
//
// The field survives across occurrences because `aggregaterule` hands the
// stored value FORWARD onto the in-flight record, the same way it ferries
// notify-ref and escalation context. It does not, and must not, delete the
// field itself: it used to, whenever its own verdict was ActionContinue, on
// the assumption that Continue meant "snooze is about to re-decide". A `close`
// breaks that assumption — this plugin passes a close straight through without
// re-stamping — so a recovery wiped the attribution and the next occurrence,
// held by the aggregate throttle, never reached this plugin to rebuild it. The
// alert sat open and un-silenced for a whole throttle window. Predicting the
// owner's decision is the bug; carrying state forward and letting the owner
// decide is the fix.
//
// Being the owner only works if the plugin actually runs on every occurrence
// that gets persisted, which is why it also implements plugins.Filter.
//
// Re-deciding on every occurrence cannot help a row that never fires again, so
// the owner also reconciles stored attributions against the filters that can
// still silence anything (ReconcileSuppression): after an API delete or edit
// of a filter, and on a minute housekeeper sweep that catches the rest — a
// time-boxed filter whose window ran out, the housekeeper's own cleanup_snooze
// deleting it, or a cluster peer re-stamping a name its cache had not yet
// dropped.
//
// Porting notes (vs src/snooze/plugins/core/snooze/plugin.py):
//
//   - Python's `Abort()` maps to plugins.ActionAbort (discard, no persist).
//   - Python's `AbortAndWrite(record=...)` maps to plugins.ActionAbortWrite
//     (persist with a fresh updated_at).
//   - The hit counter is bumped through the server's asyncwriter (the Go
//     counterpart of the Python plugin's AsyncIncrement coroutine), falling
//     back to one atomic Driver.IncMany when no writer is wired.
package snooze

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/timeconstraints"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

const (
	// collectionName is the DB collection holding snooze rules.
	collectionName = "snooze"
	// recordCollection is the alert collection this plugin attributes.
	recordCollection = "record"
	// attributionField is the record field this plugin owns: the name of the
	// rule that silenced the alert. Nothing outside this package may write or
	// delete it — see plugins.SuppressionOwner.
	attributionField = "snoozed"
)

func init() {
	plugins.Register("snooze", metaYAML, factory)
}

var (
	_ plugins.SuppressionOwner = (*Plugin)(nil)
	_ plugins.Filter           = (*Plugin)(nil)
	_ plugins.DeleteHook       = (*Plugin)(nil)
	_ plugins.UpdateHook       = (*Plugin)(nil)
)

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{
		meta: meta,
		Now:  time.Now,
	}, nil
}

// rule is the in-memory representation of one snooze record. Compiled
// condition + parsed time-constraint group sit alongside the raw fields
// the rest of the system might surface to operators.
type rule struct {
	UID         string
	Name        string
	Enabled     bool
	Discard     bool
	HitsEnabled bool
	Cond        condition.Cond
	Time        timeconstraints.Group
}

// match reports whether the rule fires for rec at moment now.
func (r rule) match(rec map[string]any, now time.Time) bool {
	if !r.Enabled {
		return false
	}
	if !condition.Match(rec, r.Cond) {
		return false
	}
	return r.Time.Match(now)
}

// Plugin is the namesake snooze Processor.
type Plugin struct {
	meta plugins.Metadata

	// Now returns the moment a record is evaluated against time
	// constraints. Defaults to time.Now; tests inject a deterministic clock.
	Now func() time.Time

	mu    sync.RWMutex
	rules map[string][]rule // tenantID → rules
	host  plugins.Host
}

// Name returns the registered plugin name.
func (p *Plugin) Name() string { return "snooze" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the Host and primes the rule cache from the database.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.mu.Lock()
	p.host = host
	p.mu.Unlock()
	return p.Reload(ctx)
}

// Reload refreshes the in-memory rule cache for the tenant in ctx from the
// snooze collection. A context with no tenant is silently skipped.
func (p *Plugin) Reload(ctx context.Context) error {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		// No driver wired in (test-only or pre-init): leave the cache empty.
		p.mu.Lock()
		p.rules = nil
		p.mu.Unlock()
		return nil
	}
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
		return nil
	}
	// Asc: true — rule evaluation is first-match-wins (see Process), so load
	// order IS priority order. An empty db.Page{} defaults to DESCENDING on
	// every driver (Mongo and sqlite both sort by insertion order but
	// Page.Asc's zero value is false), which would let a newer, broader
	// snooze silently shadow an older, more specific one whenever their
	// conditions overlap. The sibling `rule` plugin makes the same call
	// with an explicit Asc: true for the identical reason.
	docs, _, err := host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{Asc: true})
	if err != nil {
		return fmt.Errorf("snooze: load rules: %w", err)
	}
	rules := make([]rule, 0, len(docs))
	for _, d := range docs {
		r, err := docToRule(d)
		if err != nil {
			if lg := host.Logger(); lg != nil {
				lg.Warn("snooze: skipping invalid rule",
					"uid", d["uid"], "name", d["name"], "err", err)
			}
			continue
		}
		rules = append(rules, r)
	}
	p.mu.Lock()
	if p.rules == nil {
		p.rules = make(map[string][]rule)
	}
	p.rules[tenantID] = rules
	p.mu.Unlock()
	return nil
}

// SuppressionField implements plugins.SuppressionOwner: the record field this
// plugin owns. Callers that need to name the field (the retro-apply endpoint,
// for one) read it from here instead of repeating the literal.
func (p *Plugin) SuppressionField() string { return attributionField }

// Filter implements plugins.Filter: the snooze verdict is a suppression
// decision, so the pipeline must be able to ask for it even when an earlier
// processor short-circuited with an abort-and-persist verdict — which is what
// aggregaterule's throttle and anti-flapping holds do. Without it a throttled
// duplicate of a matching alert was written un-snoozed and stayed open for the
// whole throttle window, and this plugin could not own `snoozed` at all
// because it never ran.
//
// The decision is identical to an ordinary pass; the pipeline guarantees only
// one of the two runs per record. The one difference is bookkeeping: the
// occurrence was already stopped — and counted, as alert_throttled — by the
// plugin that held it, and the dashboard's Snoozed series counts where the
// pipeline stopped, so the filter pass records no alert_snoozed stat. The
// filter's Hits counter still counts it: Hits measures what a filter covers.
func (p *Plugin) Filter(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	return p.decide(ctx, rec, false)
}

// Process walks the cached rules in load order. The first enabled rule that
// matches the record and is active for the current moment wins:
//
//   - rule.Discard → ActionAbort (drop the record entirely).
//   - otherwise    → ActionAbortWrite (persist the attribution on rec).
//
// Misses fall through with ActionContinue.
//
// Every return path also settles the `snoozed` attribution this plugin owns —
// keep, set or clear, never "leave it to someone else". See the package doc
// for the table and for the outage that made the ownership explicit.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	return p.decide(ctx, rec, true)
}

// decide is the shared body of Process and Filter; recordStat says whether a
// match is recorded as an alert_snoozed stat (see Filter for why not always).
func (p *Plugin) decide(ctx context.Context, rec snoozetypes.Record, recordStat bool) (plugins.Result, error) {
	now := p.now()
	asMap := recordToMap(rec)

	tenantID, _ := auth.TenantFrom(ctx)

	p.mu.RLock()
	rules := p.rules[tenantID]
	host := p.host
	p.mu.RUnlock()

	// Close-transition pass-through: a `close` against an EXISTING aggregate
	// (aggregaterule stamps duplicates=prev+1, always >=2 once a prior
	// aggregate existed) is the pipeline retiring an alert already on the
	// books, not a new alert to filter. Suppressing it — especially a
	// discard filter's ActionAbort — drops the close write entirely and
	// wedges the row open forever. So this runs before any rule is even
	// tested, ahead of the severity-bypass block below, as the strongest
	// invariant. A first-occurrence close (duplicates < 2 — aggregaterule
	// stamps duplicates=1 when no existing aggregate matched) still runs the
	// rules below, so a fully-discarded alert's recovery event cannot leak a
	// phantom closed row.
	//
	// ATTRIBUTION: keep. This is the one branch that does not re-decide, so
	// clearing here would strip the reason a row was hidden with nothing left
	// to restore it — the shape of the original bug. An alert silenced for its
	// whole life should not resurface at the moment it recovers.
	//
	// Keeping it is also visible downstream: the recovery continues to the
	// notification plugin carrying `snoozed`, so a notification condition can
	// test it (`NOT snoozed EXISTS` skips the recovery of an alert that never
	// paged), and the stored close row keeps the attribution — the web's
	// Snoozed tab excludes closed rows, and its flow chart reads the record's
	// plugin trail rather than `snoozed` alone to tell "silenced here" from
	// "silenced earlier, recovery passed through".
	if rec.State == "close" {
		if dup, ok := toInt64(rec.Extra["duplicates"]); ok && dup >= 2 {
			return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
		}
	}

	// Global suppression-bypass: a record whose severity is listed in
	// general.snooze_bypass_severities passes straight through, before any
	// rule is tested, so a maintenance window can never silence a recovery
	// (e.g. "ok") or a configured escalation (e.g. "critical"). The list is
	// read from the DB-backed runtime settings when available (so operator
	// edits in the settings UI take effect live), falling back to the
	// boot-time file config otherwise.
	//
	// ATTRIBUTION: clear. "This severity is never silenced" has to mean the
	// record is visible, and a stale attribution carried over from a lower
	// severity would keep it hidden — an alert snoozed as a warning would stay
	// out of the alerts list after escalating to critical.
	if bypass := bypassSeverities(ctx, host); len(bypass) > 0 {
		sev := strings.ToLower(strings.TrimSpace(rec.Severity))
		for _, b := range bypass {
			if sev == b {
				p.clearAttribution(ctx, host, &rec)
				return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
			}
		}
	}

	for _, r := range rules {
		if !r.match(asMap, now) {
			continue
		}
		// ATTRIBUTION: set — this rule is now the reason the alert is
		// silenced, replacing whatever aggregaterule carried forward. The
		// pipeline's write is a merge, so an overwrite needs no explicit
		// unset. On the discard path below nothing is persisted at all, so
		// there is no attribution to settle: the stored row (if any) keeps
		// what it had and stays hidden either way.
		if rec.Extra == nil {
			rec.Extra = map[string]any{}
		}
		rec.Extra[attributionField] = r.Name

		// Persist alert_snoozed metric for dashboard aggregation.
		if recordStat {
			plugins.RecordStat(ctx, host, rec.DateEpoch, "alert_snoozed", map[string]string{"name": r.Name}, 1)
		}

		// Best-effort hit counter bump.
		if r.HitsEnabled && host != nil && host.DB() != nil && r.UID != "" {
			if err := bumpHits(ctx, host, r); err != nil && host.Logger() != nil {
				host.Logger().Warn("snooze: hit-counter update failed",
					"uid", r.UID, "name", r.Name, "err", err)
			}
		}

		if r.Discard {
			return plugins.Result{Action: plugins.ActionAbort, Record: rec}, nil
		}
		return plugins.Result{Action: plugins.ActionAbortWrite, Record: rec}, nil
	}
	// ATTRIBUTION: clear. No rule covers this record any more — because it
	// changed, because the window closed, or because the filter was deleted —
	// so it must go back to being a visible alert.
	p.clearAttribution(ctx, host, &rec)
	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// clearAttribution removes the attribution from the in-flight record and from
// the stored row.
//
// Both halves are needed: dropping the key from rec keeps it out of everything
// downstream and out of the document the pipeline writes, but that write is a
// MERGE — it cannot remove a key already in storage — so the stored row needs
// an explicit unset.
//
// It is a no-op unless the in-flight record actually carries an attribution,
// which only happens when aggregaterule ferried one forward from an existing
// row. A first occurrence therefore costs nothing, and the DB round-trip is
// paid only on the occurrence that genuinely changes the answer.
func (p *Plugin) clearAttribution(ctx context.Context, host plugins.Host, rec *snoozetypes.Record) {
	if rec.Extra == nil {
		return
	}
	if _, carried := rec.Extra[attributionField]; !carried {
		return
	}
	delete(rec.Extra, attributionField)
	if rec.UID == "" || host == nil || host.DB() == nil {
		return
	}
	if _, err := host.DB().UnsetFields(ctx, recordCollection,
		[]string{attributionField}, condition.Equals("uid", rec.UID)); err != nil {
		if lg := host.Logger(); lg != nil {
			lg.Warn("snooze: clear stale attribution",
				"uid", rec.UID, "err", err)
		}
	}
}

// ReconcileSuppression implements plugins.SuppressionOwner: for the tenant in
// ctx it clears every stored attribution that names no filter able to silence
// anything any more, and reports how many records it cleared.
//
// A filter can still silence when it exists, parses, is enabled, and its
// absolute datetime window is not wholly in the past — the same test that
// retires a filter in the pipeline for good. A recurring window that is merely
// closed at this moment (a nightly maintenance slot, at noon) still counts: its
// rows re-decide on their next occurrence, and clearing them here would pull
// every quiet overnight alert into the list each morning.
//
// The live set is read from the database, not the in-memory cache, so the
// answer never lags a syncer reload.
func (p *Plugin) ReconcileSuppression(ctx context.Context) (int, error) {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		return 0, nil
	}
	docs, _, err := host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{})
	if err != nil {
		return 0, fmt.Errorf("snooze: reconcile: list filters: %w", err)
	}
	now := p.now()
	var live []condition.Cond
	for _, d := range docs {
		r, err := docToRule(d)
		if err != nil || !r.Enabled || r.Name == "" {
			continue
		}
		if status, _ := classify(d["time_constraints"], now); status == "" || status == statusExpired {
			continue
		}
		live = append(live, condition.Equals(attributionField, r.Name))
	}
	stale := condition.Exists(attributionField)
	if len(live) > 0 {
		stale = condition.And(stale, condition.Not(condition.Or(live...)))
	}
	n, err := host.DB().UnsetFields(ctx, recordCollection, []string{attributionField}, stale)
	if err != nil {
		return 0, fmt.Errorf("snooze: reconcile: clear stale attribution: %w", err)
	}
	if n > 0 {
		if lg := host.Logger(); lg != nil {
			lg.Info("snooze: cleared attribution of filters that no longer silence", "records", n)
		}
	}
	return n, nil
}

// AfterDelete reconciles as soon as a filter is deleted through the API, so
// the rows it silenced return to the alerts list at once instead of on the
// next housekeeper sweep. A record that is still firing would re-decide on its
// next occurrence anyway; this is for the ones that never fire again, which
// otherwise stayed hidden for good behind a filter that no longer exists —
// deleting one broad filter left 305 such rows on the live server, one of them
// an open critical.
func (p *Plugin) AfterDelete(ctx context.Context, _ []string) error {
	_, err := p.ReconcileSuppression(ctx)
	return err
}

// AfterUpdate reconciles after an API edit of a filter: a rename orphans the
// rows attributed under the old name, and disabling a filter or ending its
// window means it silences nothing any more.
func (p *Plugin) AfterUpdate(ctx context.Context, _ string, _ map[string]any) error {
	_, err := p.ReconcileSuppression(ctx)
	return err
}

// bypassSeverities returns the current general.snooze_bypass_severities list
// (already lowercased) for the tenant in ctx. It prefers the DB-backed
// runtime settings store — the settings UI writes there, and the runtime
// store already handles per-tenant caching + invalidation — falling back to
// the boot-time file config when host doesn't expose runtime settings, the
// settings snapshot itself is nil, or the read errors.
func bypassSeverities(ctx context.Context, host plugins.Host) []string {
	if rsHost, ok := host.(plugins.RuntimeSettingsHost); ok {
		if rs := rsHost.RuntimeSettings(); rs != nil {
			if general, err := rs.General(ctx); err == nil {
				return general.SnoozeBySeverities
			}
		}
	}
	if host == nil {
		return nil
	}
	cfg := host.Config()
	if cfg == nil {
		return nil
	}
	return cfg.General.SnoozeBySeverities
}

// cachedRules returns a copy of the cached rule set for tenantID. Test-only convenience.
func (p *Plugin) cachedRules(tenantID string) []rule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]rule, len(p.rules[tenantID]))
	copy(out, p.rules[tenantID])
	return out
}

func (p *Plugin) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// bumpHits increments the `hits` field on the matching snooze rule.
//
// With the server's asyncwriter available the bump is coalesced there: hits
// arrive in storms (throttled repeats reach this plugin too), and two
// synchronous round-trips per match on the ingest path are exactly the cost a
// storm cannot afford. The shared writer upserts — the stats counters rely on
// it — so this goes through IncrementExisting: a filter deleted between the
// match and the flush must not come back as a phantom document, which, having
// no condition, would match every alert. Without a writer (tests, tools) it
// falls back to one atomic IncMany.
func bumpHits(ctx context.Context, host plugins.Host, r rule) error {
	if wh, ok := host.(plugins.AsyncWriterHost); ok {
		if w := wh.AsyncWriter(); w != nil {
			w.IncrementExisting(ctx, collectionName, "hits", db.Document{"uid": r.UID}, 1)
			return nil
		}
	}
	_, err := host.DB().IncMany(ctx, collectionName, "hits", condition.Equals("uid", r.UID), 1)
	return err
}

// docToRule maps a raw snooze document into a parsed rule. Unknown or
// malformed fields fall back to safe defaults (enabled=true, hits=true).
func docToRule(d db.Document) (rule, error) {
	r := rule{
		Enabled:     true,
		HitsEnabled: true,
	}
	if v, ok := d["uid"].(string); ok {
		r.UID = v
	}
	if v, ok := d["name"].(string); ok {
		r.Name = v
	}
	if v, ok := d["enabled"].(bool); ok {
		r.Enabled = v
	}
	if v, ok := d["discard"].(bool); ok {
		r.Discard = v
	}
	if v, ok := d["hits_enabled"].(bool); ok {
		r.HitsEnabled = v
	}

	// Condition: accept legacy list form or object form.
	if raw, present := d["condition"]; present {
		c, err := parseCondition(raw)
		if err != nil {
			return rule{}, fmt.Errorf("condition: %w", err)
		}
		r.Cond = c
	}

	// Time constraints: object form matching timeconstraints.Group JSON.
	if raw, present := d["time_constraints"]; present && raw != nil {
		g, err := parseTimeConstraints(raw)
		if err != nil {
			return rule{}, fmt.Errorf("time_constraints: %w", err)
		}
		r.Time = g
	}
	return r, nil
}

// parseCondition handles either the legacy ['=', 'a', 1] list form or the
// {"op": "=", ...} object form, both of which can survive a JSON round-trip.
func parseCondition(raw any) (condition.Cond, error) {
	switch v := raw.(type) {
	case nil:
		return condition.Cond{}, nil
	case []any:
		return condition.FromList(v)
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return condition.Cond{}, err
		}
		var c condition.Cond
		if err := json.Unmarshal(b, &c); err != nil {
			return condition.Cond{}, err
		}
		return c, nil
	default:
		return condition.Cond{}, fmt.Errorf("unsupported shape %T", raw)
	}
}

func parseTimeConstraints(raw any) (timeconstraints.Group, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return timeconstraints.Group{}, err
	}
	var g timeconstraints.Group
	if err := json.Unmarshal(b, &g); err != nil {
		return timeconstraints.Group{}, err
	}
	return g, nil
}

// recordToMap flattens a typed Record into the loose map shape the
// condition evaluator consumes. Mirrors core.recordToDoc (not exported)
// closely enough for condition matching; updates to that helper should be
// reflected here.
func recordToMap(rec snoozetypes.Record) map[string]any {
	m := map[string]any{}
	if rec.UID != "" {
		m["uid"] = rec.UID
	}
	if rec.Host != "" {
		m["host"] = rec.Host
	}
	if rec.Source != "" {
		m["source"] = rec.Source
	}
	if rec.Process != "" {
		m["process"] = rec.Process
	}
	if rec.Severity != "" {
		m["severity"] = rec.Severity
	}
	if rec.Message != "" {
		m["message"] = rec.Message
	}
	if !rec.Timestamp.IsZero() {
		m["timestamp"] = rec.Timestamp
	}
	if rec.DateEpoch != 0 {
		m["date_epoch"] = rec.DateEpoch
	}
	if rec.TTL != 0 {
		m["ttl"] = rec.TTL
	}
	if rec.Environment != "" {
		m["environment"] = rec.Environment
	}
	if rec.Hash != "" {
		m["hash"] = rec.Hash
	}
	if len(rec.Tags) > 0 {
		m["tags"] = rec.Tags
	}
	if len(rec.Raw) > 0 {
		m["raw"] = rec.Raw
	}
	if rec.State != "" {
		m["state"] = rec.State
	}
	if len(rec.Plugins) > 0 {
		m["plugins"] = rec.Plugins
	}
	if rec.EscalationCount != 0 {
		m["escalation_count"] = rec.EscalationCount
	}
	if rec.EscalationReason != "" {
		m["escalation_reason"] = rec.EscalationReason
	}
	for k, v := range rec.Extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return m
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true //nolint:gosec
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true //nolint:gosec
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}
