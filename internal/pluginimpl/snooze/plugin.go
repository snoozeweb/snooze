// Package snooze implements the namesake "snooze" Processor plugin: a record
// pipeline filter that silences matching alerts during configured time
// windows. The plugin caches its rule set in memory (refreshed on PostInit
// and Reload from the "snooze" collection) and bumps a per-rule hit counter
// on each match.
//
// Porting notes (vs src/snooze/plugins/core/snooze/plugin.py):
//
//   - Python's `Abort()` maps to plugins.ActionAbort (discard, no persist).
//   - Python's `AbortAndWrite(record=...)` maps to plugins.ActionAbortWrite
//     (persist with a fresh updated_at).
//   - The hit counter is bumped via a synchronous Driver.UpdateOne fetched
//     by name, rather than via the AsyncIncrement coroutine the Python
//     plugin uses. Trade-off: one extra round-trip per match. The
//     asyncwriter package is available; promoting this to a batched flush
//     is a follow-up.
package snooze

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/internal/timeconstraints"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

// collectionName is the DB collection holding snooze rules.
const collectionName = "snooze"

func init() {
	plugins.Register("snooze", metaYAML, factory)
}

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
	rules []rule
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

// Reload refreshes the in-memory rule cache from the snooze collection.
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
	docs, _, err := host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{})
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
	p.rules = rules
	p.mu.Unlock()
	return nil
}

// Process walks the cached rules in load order. The first enabled rule that
// matches the record and is active for the current moment wins:
//
//   - rule.Discard → ActionAbort (drop the record entirely).
//   - otherwise    → ActionAbortWrite (persist a `snoozed` field on rec).
//
// Misses fall through with ActionContinue and an unchanged record.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	now := p.now()
	asMap := recordToMap(rec)

	p.mu.RLock()
	rules := p.rules
	host := p.host
	p.mu.RUnlock()

	for _, r := range rules {
		if !r.match(asMap, now) {
			continue
		}
		// Tag the record so downstream sees the snooze attribution.
		if rec.Extra == nil {
			rec.Extra = map[string]any{}
		}
		rec.Extra["snoozed"] = r.Name

		// Best-effort hit counter bump (synchronous UpdateOne).
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
	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// cachedRules returns a copy of the current cached rule set. Test-only convenience.
func (p *Plugin) cachedRules() []rule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]rule, len(p.rules))
	copy(out, p.rules)
	return out
}

func (p *Plugin) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// bumpHits increments the `hits` field on the matching snooze rule. The
// driver does not expose a typed counter increment for a single UID, so we
// fetch the current value, +1, and write it back. Concurrent matches may
// undercount: acceptable for an audit hint, and a follow-up can swap this
// for an asyncwriter-backed batch.
func bumpHits(ctx context.Context, host plugins.Host, r rule) error {
	doc, err := host.DB().GetOne(ctx, collectionName, db.Document{"uid": r.UID})
	if err != nil {
		return err
	}
	current, _ := toInt64(doc["hits"])
	return host.DB().UpdateOne(ctx, collectionName, r.UID,
		db.Document{"hits": current + 1}, false)
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
