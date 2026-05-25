// Package rule implements the rule Processor plugin.
//
// Rules filter records by a Condition. When a rule matches, its list of
// Modifications is applied to the record, and the rule's children rules are
// recursively evaluated against the (possibly mutated) record. This plugin
// is purely transformative — it never aborts the pipeline and always returns
// plugins.ActionContinue.
//
// The plugin caches the rule tree at PostInit and refreshes it on Reload (the
// syncer fires Reload when a write to the "rule" collection occurs).
//
// Port of src/snooze/plugins/core/rule/plugin.py.
package rule

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/modification"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

// ruleCollection is the database collection backing this plugin.
const ruleCollection = "rule"

// orderByField is the rule field used to order entries within a tree level.
// The Python metadata.yaml stores this as `force_order: 'tree_order'`. The
// Go plugins.Metadata.ForceOrder field is an int (a different semantic), so
// we hardcode the field name here. See metadata.yaml for the matching note.
const orderByField = "tree_order"

func init() {
	plugins.Register("rule", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the rule Processor implementation.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	mu    sync.RWMutex
	rules []*ruleEntry
}

// ruleEntry is a single node in the in-memory rule tree.
type ruleEntry struct {
	uid           string
	name          string
	enabled       bool
	cond          condition.Cond
	modifications []modification.Modification
	// kvSets is the list of KV_SET modifications, deferred from the
	// internal/modification package because they require host access.
	// Each entry holds {Dict, Key, OutField} resolved at apply time.
	kvSets   []kvSet
	children []*ruleEntry
}

// kvSet captures a `KV_SET` modification. Wire shape:
//
//	["KV_SET", dict, key, out_field]
//
// Semantics (matching the Python original in src/snooze/utils/modification.py):
// look up kv[Dict][record[Key]] and assign the resulting value to
// record[OutField]. `Key` is the *record field name* whose value drives the
// kv lookup, not a literal key string.
type kvSet struct {
	Dict     string
	Key      string
	OutField string
}

// kvGetter is the duck-typed interface the rule plugin uses to consult the
// in-memory cache of the "kv" plugin (internal/pluginimpl/kv). Importing kv
// directly would couple two sibling pluginimpl packages; structural typing
// against Host.Plugin("kv") avoids that without losing the cache hit on the
// hot path. The DB fallback in applyKVSets covers tests and edge cases where
// the plugin handle is unavailable.
type kvGetter interface {
	Get(dict, key string) (any, bool)
}

// Name returns the registered plugin name.
func (p *Plugin) Name() string { return "rule" }

// Metadata returns the parsed metadata descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the Host and performs the initial cache load.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.host = host
	return p.Reload(ctx)
}

// Reload rebuilds the cached rule tree from the database.
func (p *Plugin) Reload(ctx context.Context) error {
	if p.host == nil || p.host.DB() == nil {
		// No DB wired (test fast path) — start empty.
		p.mu.Lock()
		p.rules = nil
		p.mu.Unlock()
		return nil
	}
	tree, err := p.loadChildren(ctx, "")
	if err != nil {
		return fmt.Errorf("rule: reload: %w", err)
	}
	p.mu.Lock()
	p.rules = tree
	p.mu.Unlock()
	return nil
}

// loadChildren returns the rule entries whose parent is `parentUID`. When
// parentUID is empty, the top-level rules (records without a `parents` field)
// are returned.
func (p *Plugin) loadChildren(ctx context.Context, parentUID string) ([]*ruleEntry, error) {
	driver := p.host.DB()
	var cond condition.Cond
	if parentUID == "" {
		// Mirrors the Python ``['NOT', ['EXISTS', 'parents']]`` query.
		cond = condition.Not(condition.Exists("parents"))
	} else {
		// Mirrors ``['IN', uid, 'parents']``: the parents array contains uid.
		cond = condition.Cond{Op: condition.OpIn, Field: "parents", Value: parentUID}
	}
	docs, _, err := driver.Search(ctx, ruleCollection, cond, db.Page{OrderBy: orderByField, Asc: true})
	if err != nil {
		return nil, err
	}
	out := make([]*ruleEntry, 0, len(docs))
	for _, doc := range docs {
		entry, err := p.buildEntry(ctx, doc)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// buildEntry converts a stored rule document into a ruleEntry and recursively
// loads its children.
func (p *Plugin) buildEntry(ctx context.Context, doc db.Document) (*ruleEntry, error) {
	uid, _ := doc["uid"].(string)
	name, _ := doc["name"].(string)
	enabled := true
	if v, ok := doc["enabled"].(bool); ok {
		enabled = v
	}

	cond, err := parseCondition(doc["condition"])
	if err != nil {
		return nil, fmt.Errorf("rule %q: %w", name, err)
	}

	mods, kvs, err := parseModifications(doc["modifications"])
	if err != nil {
		return nil, fmt.Errorf("rule %q: %w", name, err)
	}

	e := &ruleEntry{
		uid:           uid,
		name:          name,
		enabled:       enabled,
		cond:          cond,
		modifications: mods,
		kvSets:        kvs,
	}
	if uid != "" {
		kids, err := p.loadChildren(ctx, uid)
		if err != nil {
			return nil, err
		}
		e.children = kids
	}
	return e, nil
}

// Process walks the cached rule tree against rec.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	p.mu.RLock()
	rules := p.rules
	p.mu.RUnlock()

	view := recordToMap(rec)
	p.processRules(ctx, view, rules)
	out := mapToRecord(rec, view)
	return plugins.Result{Action: plugins.ActionContinue, Record: out}, nil
}

// processRules iterates rules and recurses into matched ones. Mirrors the
// Python Rule.process_rules method.
func (p *Plugin) processRules(ctx context.Context, view map[string]any, rules []*ruleEntry) {
	for _, r := range rules {
		if !r.enabled {
			continue
		}
		if !condition.Match(view, r.cond) {
			continue
		}
		appendRuleTag(view, r.name)
		_ = applyModifications(view, r.modifications)
		p.applyKVSets(ctx, view, r.kvSets)
		p.processRules(ctx, view, r.children)
	}
}

// applyModifications runs the standard modifications against view, ignoring
// per-op errors the way Python does (each modify() returns a bool, exceptions
// are swallowed by the engine).
func applyModifications(view map[string]any, mods []modification.Modification) bool {
	modified := false
	for _, m := range mods {
		ok, err := modification.Apply(view, m)
		if err != nil {
			continue
		}
		if ok {
			modified = true
		}
	}
	return modified
}

// applyKVSets implements the KV_SET op that was deferred from
// internal/modification (it needs host access).
//
// For each {Dict, Key, OutField} entry it reads view[Key], uses that as the
// lookup key against kv[Dict], and writes view[OutField] = looked-up value.
// This mirrors the Python original (src/snooze/utils/modification.py KvSet):
//
//	record[out_field] = kv[dict][record[key]]
//
// The lookup prefers the kv plugin's in-memory cache (via the kvGetter
// duck-type) so we don't pay a DB round-trip per record; the GetOne fallback
// is here for tests and the rare boot window where PostInit hasn't yet wired
// the kv handle.
func (p *Plugin) applyKVSets(ctx context.Context, view map[string]any, sets []kvSet) {
	if len(sets) == 0 {
		return
	}
	if p.host == nil {
		return
	}
	var getter kvGetter
	if kp := p.host.Plugin("kv"); kp != nil {
		getter, _ = kp.(kvGetter)
	}
	var dbCtx context.Context
	var cancel context.CancelFunc
	for _, s := range sets {
		if s.OutField == "" || s.Key == "" {
			continue
		}
		raw, ok := view[s.Key]
		if !ok {
			continue
		}
		recordKey, ok := raw.(string)
		if !ok {
			recordKey = fmt.Sprint(raw)
		}
		if recordKey == "" {
			continue
		}
		if getter != nil {
			if v, found := getter.Get(s.Dict, recordKey); found {
				view[s.OutField] = v
			}
			continue
		}
		if p.host.DB() == nil {
			continue
		}
		if dbCtx == nil {
			// Best-effort timeout so a slow KV backend doesn't stall a record.
			dbCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
		}
		doc, err := p.host.DB().GetOne(dbCtx, "kv", db.Document{"dict": s.Dict, "key": recordKey})
		if err != nil || doc == nil {
			continue
		}
		if v, ok := doc["value"]; ok {
			view[s.OutField] = v
		}
	}
}

// appendRuleTag merges r.name into view["rules"] without duplicates. Mirrors
// Python's RuleObject.match side-effect.
func appendRuleTag(view map[string]any, name string) {
	cur, _ := view["rules"].([]any)
	for _, existing := range cur {
		if existing == name {
			return
		}
	}
	view["rules"] = append(cur, name)
}

// parseCondition tolerates both the legacy list form and the new object form.
// nil/missing yields the AlwaysTrue zero value (matching Python's
// get_condition(None)).
//
// The object form is re-marshalled into JSON so condition.Cond.UnmarshalJSON
// owns the wire-shape normalisation (op vs type key, EQUALS → "=", etc.).
// Duplicating that logic here previously left the rule plugin reading
// frontend-posted conditions as AlwaysTrue.
func parseCondition(raw any) (condition.Cond, error) {
	if raw == nil {
		return condition.Cond{}, nil
	}
	switch v := raw.(type) {
	case []any:
		return condition.FromList(v)
	case map[string]any:
		buf, err := json.Marshal(v)
		if err != nil {
			return condition.Cond{}, fmt.Errorf("condition: marshal: %w", err)
		}
		var c condition.Cond
		if err := c.UnmarshalJSON(buf); err != nil {
			return condition.Cond{}, err
		}
		return c, nil
	}
	return condition.Cond{}, fmt.Errorf("unsupported condition shape: %T", raw)
}

// parseModifications splits the raw mods list into (regular, kv_set) pairs.
// KV_SET is recognised and routed to a separate slice because the standard
// modification package intentionally doesn't implement it.
//
// The canonical wire/storage shape is the positional Python-era form
// ["OP", "field", arg…]. The React rule editor de/serialises its
// discriminated-union TS type at the network boundary (see
// web/src/shared/modifications/wire.ts) so this side never sees the
// object form. modification.Modification (and the rest of the Go layer)
// is built around the positional shape.
func parseModifications(raw any) ([]modification.Modification, []kvSet, error) {
	if raw == nil {
		return nil, nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("modifications must be a list, got %T", raw)
	}
	var mods []modification.Modification
	var kvs []kvSet
	for i, entry := range list {
		args, ok := entry.([]any)
		if !ok {
			return nil, nil, fmt.Errorf("modifications[%d]: not a list (%T)", i, entry)
		}
		if len(args) > 0 {
			if op, ok := args[0].(string); ok && op == "KV_SET" {
				// Wire form is the Python-era positional shape
				// ["KV_SET", dict, key, out_field]: look up kv[dict][record[key]]
				// and assign the result to record[out_field].
				dict, _ := safeString(args, 1)
				key, _ := safeString(args, 2)
				outField, _ := safeString(args, 3)
				kvs = append(kvs, kvSet{Dict: dict, Key: key, OutField: outField})
				continue
			}
		}
		m, err := modification.Parse(args)
		if err != nil {
			return nil, nil, fmt.Errorf("modifications[%d]: %w", i, err)
		}
		mods = append(mods, m)
	}
	return mods, kvs, nil
}

// safeString fetches args[i] as a string, returning ("", false) when out of
// range or not a string.
func safeString(args []any, i int) (string, bool) {
	if i >= len(args) {
		return "", false
	}
	s, ok := args[i].(string)
	return s, ok
}

// recordToMap projects a Record into the flat map view the condition and
// modification packages consume.
func recordToMap(rec snoozetypes.Record) map[string]any {
	d := map[string]any{}
	if rec.UID != "" {
		d["uid"] = rec.UID
	}
	if rec.Host != "" {
		d["host"] = rec.Host
	}
	if rec.Source != "" {
		d["source"] = rec.Source
	}
	if rec.Process != "" {
		d["process"] = rec.Process
	}
	if rec.Severity != "" {
		d["severity"] = rec.Severity
	}
	if rec.Message != "" {
		d["message"] = rec.Message
	}
	if !rec.Timestamp.IsZero() {
		d["timestamp"] = rec.Timestamp
	}
	if rec.DateEpoch != 0 {
		d["date_epoch"] = rec.DateEpoch
	}
	if rec.TTL != 0 {
		d["ttl"] = rec.TTL
	}
	if rec.Environment != "" {
		d["environment"] = rec.Environment
	}
	if rec.Hash != "" {
		d["hash"] = rec.Hash
	}
	if len(rec.Tags) > 0 {
		d["tags"] = rec.Tags
	}
	if len(rec.Raw) > 0 {
		d["raw"] = rec.Raw
	}
	if rec.State != "" {
		d["state"] = rec.State
	}
	if len(rec.Plugins) > 0 {
		d["plugins"] = rec.Plugins
	}
	for k, v := range rec.Extra {
		if _, exists := d[k]; !exists {
			d[k] = v
		}
	}
	return d
}

// mapToRecord rebuilds a Record from a mutated view. Known typed keys map to
// their typed fields; everything else lands in Extra. A key missing from the
// view means a DELETE modification removed it, so we deliberately leave the
// corresponding typed field zero. orig is reserved for a future "carry over
// untouched fields" optimisation and is currently unused.
func mapToRecord(_ snoozetypes.Record, view map[string]any) snoozetypes.Record {
	out := snoozetypes.Record{Extra: map[string]any{}}
	// Default fields back to orig for fields not seen in view (i.e. removed
	// by a DELETE modification). For typed fields we treat absence as "clear"
	// so a DELETE on host actually clears Host. This matches the Python
	// semantic of `del record[key]`.
	for k, v := range view {
		switch k {
		case "uid":
			out.UID, _ = v.(string)
		case "host":
			out.Host, _ = v.(string)
		case "source":
			out.Source, _ = v.(string)
		case "process":
			out.Process, _ = v.(string)
		case "severity":
			out.Severity, _ = v.(string)
		case "message":
			out.Message, _ = v.(string)
		case "timestamp":
			if t, ok := v.(time.Time); ok {
				out.Timestamp = t
			}
		case "date_epoch":
			out.DateEpoch = toInt64(v)
		case "ttl":
			out.TTL = toInt64(v)
		case "environment":
			out.Environment, _ = v.(string)
		case "tags":
			out.Tags = toStringSlice(v)
		case "raw":
			if m, ok := v.(map[string]any); ok {
				out.Raw = m
			}
		case "state":
			out.State, _ = v.(string)
		case "hash":
			out.Hash, _ = v.(string)
		case "plugins":
			out.Plugins = toStringSlice(v)
		default:
			out.Extra[k] = v
		}
	}
	if len(out.Extra) == 0 {
		out.Extra = nil
	}
	return out
}

// toInt64 narrows a JSON-decoded numeric value to int64.
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

// toStringSlice coerces a []any (or []string) to []string.
func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
