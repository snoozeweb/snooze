// Package reject implements the "reject" Processor plugin: a pre-persistence
// policy gate that aborts inbound alerts matching any enabled rule and signals
// HTTP 422 to the sender via the reject_policy field on the output map.
//
// Rule evaluation order mirrors insertion order returned by the DB (lexicographic
// by name in the default SQLite/Mongo sort). The first matching enabled rule
// wins; subsequent rules are not evaluated.
//
// Pipeline position: "reject" sorts before "rule" and "snooze" in the
// lexicographic processOrder used by core/boot.go, so the gate fires first.
package reject

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

// collectionName is the DB collection holding reject policy rules.
const collectionName = "reject"

func init() {
	plugins.Register("reject", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// rejectRule is the in-memory representation of one reject policy rule.
type rejectRule struct {
	Name    string
	Enabled bool
	Cond    condition.Cond
}

// Plugin is the reject Processor. It implements Plugin, DataModel, and Processor.
type Plugin struct {
	meta plugins.Metadata

	mu    sync.RWMutex
	rules map[string][]rejectRule // tenantID → rules
	host  plugins.Host
}

// Name returns the registered plugin name.
func (p *Plugin) Name() string { return "reject" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the Host and primes the rule cache from the database.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.mu.Lock()
	p.host = host
	p.mu.Unlock()
	return p.Reload(ctx)
}

// Reload refreshes the in-memory rule cache for the tenant in ctx from the
// reject collection. A context with no tenant is silently skipped.
func (p *Plugin) Reload(ctx context.Context) error {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		p.mu.Lock()
		p.rules = nil
		p.mu.Unlock()
		return nil
	}
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
		return nil
	}
	docs, _, err := host.DB().Search(ctx, collectionName, condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("reject: load rules: %w", err)
	}
	rules := make([]rejectRule, 0, len(docs))
	for _, d := range docs {
		r, err := docToRule(d)
		if err != nil {
			if lg := host.Logger(); lg != nil {
				lg.Warn("reject: skipping invalid rule",
					"uid", d["uid"], "name", d["name"], "err", err)
			}
			continue
		}
		rules = append(rules, r)
	}
	p.mu.Lock()
	if p.rules == nil {
		p.rules = make(map[string][]rejectRule)
	}
	p.rules[tenantID] = rules
	p.mu.Unlock()
	return nil
}

// Process walks the cached rules in load order. The first enabled rule whose
// condition matches the record causes an abort:
//
//   - Sets rec.Extra["reject_policy"] = ruleName.
//   - Returns (Result{Action: ActionAbort, Record: rec}, nil).
//
// Returning nil error (not a sentinel error) prevents the pipeline from
// triggering its forensic exception-write path. The rejection reason is
// communicated via the record map field so ProcessRecordMap can propagate it.
//
// A miss (no enabled rule matches) returns ActionContinue.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	asMap := recordToMap(rec)

	tenantID, _ := auth.TenantFrom(ctx)

	p.mu.RLock()
	rules := p.rules[tenantID]
	p.mu.RUnlock()

	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if !condition.Match(asMap, r.Cond) {
			continue
		}
		if rec.Extra == nil {
			rec.Extra = map[string]any{}
		}
		rec.Extra["reject_policy"] = r.Name
		return plugins.Result{Action: plugins.ActionAbort, Record: rec}, nil
	}
	return plugins.Result{Action: plugins.ActionContinue, Record: rec}, nil
}

// Schema implements DataModel. Returns nil (no JSON Schema validation beyond
// the primary-key uniqueness enforced by the CRUD layer).
func (p *Plugin) Schema() any { return nil }

// Validate implements DataModel. All documents with a name field are accepted.
func (p *Plugin) Validate(obj map[string]any) error {
	if _, ok := obj["name"].(string); !ok || obj["name"] == "" {
		return fmt.Errorf("reject: rule must have a non-empty 'name' field")
	}
	return nil
}

// cachedRules returns a copy of the cached rule set for tenantID. Test-only convenience.
func (p *Plugin) cachedRules(tenantID string) []rejectRule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]rejectRule, len(p.rules[tenantID]))
	copy(out, p.rules[tenantID])
	return out
}

// docToRule maps a raw reject document into a parsed rejectRule. Unknown or
// malformed fields fall back to safe defaults (enabled=true).
func docToRule(d db.Document) (rejectRule, error) {
	r := rejectRule{Enabled: true}
	if v, ok := d["name"].(string); ok {
		r.Name = v
	}
	if v, ok := d["enabled"].(bool); ok {
		r.Enabled = v
	}
	if raw, present := d["condition"]; present {
		c, err := parseCondition(raw)
		if err != nil {
			return rejectRule{}, fmt.Errorf("condition: %w", err)
		}
		r.Cond = c
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
		return condition.Cond{}, fmt.Errorf("unsupported condition shape %T", raw)
	}
}

// recordToMap flattens a typed Record into the loose map shape the condition
// evaluator consumes. Mirrors internal/pluginimpl/snooze.recordToMap.
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
	for k, v := range rec.Extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return m
}
