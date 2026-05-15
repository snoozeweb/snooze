// Package aggregaterule implements the Snooze "aggregate rule" Processor
// plugin. It groups incoming records by a configurable fingerprint (the
// rule's `fields`) so duplicates collapse onto a single aggregate row whose
// `duplicates` counter is bumped through the async writer.
//
// Behaviour summary (ported from src/snooze/plugins/core/aggregaterule):
//
//   - On PostInit / Reload, the plugin loads every aggregate-rule document
//     from the `aggregaterule` collection into an in-memory snapshot, with
//     each rule's condition pre-compiled.
//   - PostInit seeds a single `_default` rule (fingerprint
//     [host, source, message]) when the collection is empty.
//   - Process walks the loaded rules in order. The first rule whose
//     condition matches the record determines the aggregate. A SHA-1 hash
//     (rule name + sorted field=value pairs) is the cross-record identity.
//   - If an existing record with the same hash exists in the `record`
//     collection, the plugin merges identifying state from it onto the
//     incoming record, bumps `duplicates`, and decides the pipeline verdict:
//     ActionAbortUpdate inside the throttle window, ActionContinue
//     otherwise. State transitions (close, ack/esc, flapping) follow the
//     Python implementation's logic.
//   - When no rule matches, a default-hash bucket is used so every record
//     still aggregates.
//
// The plugin uses `internal/db/asyncwriter` to merge duplicate-counter
// increments into bulk updates against the `record` collection.
package aggregaterule

import (
	"context"
	"crypto/sha1" //nolint:gosec
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/db/asyncwriter"
	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

const (
	// ruleCollection is the storage collection for aggregate-rule definitions.
	ruleCollection = "aggregaterule"
	// recordCollection is the storage collection for aggregated alert records.
	recordCollection = "record"

	defaultThrottle = int64(10)
	defaultFlapping = int64(3)
)

func init() {
	plugins.Register("aggregaterule", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the aggregate-rule Processor.
type Plugin struct {
	meta plugins.Metadata

	mu    sync.RWMutex
	rules []*compiledRule
	host  plugins.Host
	// clock is overrideable for tests.
	clock func() time.Time
}

// compiledRule is the in-memory, pre-evaluated form of a stored aggregate rule.
type compiledRule struct {
	name     string
	enabled  bool
	cond     *condition.Compiled
	fields   []string
	watch    []string
	throttle int64
	flapping int64
}

// Name returns the registered plugin identifier.
func (p *Plugin) Name() string { return "aggregaterule" }

// Metadata returns the static descriptor parsed from metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit seeds the default aggregate rule when the collection is empty and
// then loads the rules snapshot. It is called once by the plugin registry
// after every plugin has been instantiated.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.mu.Lock()
	p.host = host
	if p.clock == nil {
		p.clock = time.Now
	}
	p.mu.Unlock()
	if err := p.seedDefault(ctx, host); err != nil {
		return fmt.Errorf("aggregaterule: seed default: %w", err)
	}
	return p.Reload(ctx)
}

// Reload refreshes the in-memory aggregate-rule snapshot from the database.
func (p *Plugin) Reload(ctx context.Context) error {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		return nil
	}
	docs, _, err := host.DB().Search(ctx, ruleCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("aggregaterule: reload: %w", err)
	}
	rules := make([]*compiledRule, 0, len(docs))
	for _, d := range docs {
		r, cerr := compileRule(d)
		if cerr != nil {
			// Skip malformed rules; the API layer rejects them at write time,
			// so this is a defence-in-depth log path. We don't have a logger
			// here without host wiring, so silently skip.
			continue
		}
		rules = append(rules, r)
	}
	p.mu.Lock()
	p.rules = rules
	p.mu.Unlock()
	return nil
}

// Process implements plugins.Processor. It assigns the record a hash based on
// the first matching rule (or a default-bucket hash) and decides the verdict.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	p.mu.RLock()
	rules := p.rules
	host := p.host
	now := p.clock
	p.mu.RUnlock()
	if now == nil {
		now = time.Now
	}

	recMap := recordToMap(rec)

	var (
		matched   *compiledRule
		aggrName  string
		fields    []string
		throttle  int64
		flapping  int64
		watchKeys []string
	)
	for _, r := range rules {
		if !r.enabled {
			continue
		}
		if r.cond != nil && !r.cond.Match(recMap) {
			continue
		}
		matched = r
		aggrName = r.name
		fields = r.fields
		throttle = r.throttle
		flapping = r.flapping
		watchKeys = r.watch
		break
	}

	if matched != nil {
		recMap["aggregate"] = aggrName
		recMap["hash"] = computeHash(aggrName, fields, recMap)
	} else {
		// Default bucket: hash the raw payload (or whole record as fallback)
		// so identical-shape records still aggregate.
		aggrName = "default"
		throttle = defaultThrottle
		flapping = defaultFlapping
		recMap["aggregate"] = aggrName
		recMap["hash"] = defaultHash(recMap)
	}

	out, action, err := p.matchAggregate(ctx, host, recMap, aggrName, throttle, flapping, watchKeys, now())
	if err != nil {
		return plugins.Result{Action: plugins.ActionAbort, Record: rec}, err
	}
	mergeMapIntoRecord(&rec, out)
	return plugins.Result{Action: action, Record: rec}, nil
}

// matchAggregate looks up the existing aggregate row by hash and decides
// the verdict. Returns the (possibly mutated) record map and the action.
func (p *Plugin) matchAggregate(
	ctx context.Context,
	host plugins.Host,
	rec map[string]any,
	_ string,
	throttle int64,
	flapping int64,
	watch []string,
	now time.Time,
) (map[string]any, plugins.Action, error) {
	if host == nil || host.DB() == nil {
		// In tests with no DB the plugin is a no-op pass-through.
		rec["duplicates"] = int64(1)
		return rec, plugins.ActionContinue, nil
	}

	hash, _ := rec["hash"].(string)
	hashStr := hash
	existing, err := host.DB().GetOne(ctx, recordCollection, db.Document{"hash": hashStr})
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return rec, plugins.ActionAbort, fmt.Errorf("aggregaterule: lookup record: %w", err)
	}
	if existing == nil {
		// First occurrence: mark and pass through.
		rec["duplicates"] = int64(1)
		return rec, plugins.ActionContinue, nil
	}

	// Merge fields: existing values win for identity (uid, date_epoch,
	// state, duplicates) but the incoming record's payload otherwise
	// overwrites.
	prevDup := toInt64(existing["duplicates"], 0)
	prevDate := toInt64(existing["date_epoch"], now.Unix())
	prevState, _ := existing["state"].(string)
	prevUID, _ := existing["uid"].(string)
	commentCount := toInt64(existing["comment_count"], 0)
	flappingCountdown, hasFlap := toInt64WithOk(existing["flapping_countdown"])

	incomingState, _ := rec["state"].(string)
	rec["uid"] = prevUID
	rec["duplicates"] = prevDup + 1
	rec["date_epoch"] = prevDate
	rec["comment_count"] = commentCount

	throttling := throttle < 0 || (now.Unix()-prevDate < throttle)

	// Close-state handling: an incoming `close` against a still-open aggregate
	// closes it; against an already-closed aggregate it's a duplicate.
	if incomingState == "close" {
		if prevState != "close" {
			rec["state"] = "close"
			rec["comment_count"] = commentCount + 1
			return rec, plugins.ActionContinue, nil
		}
		// Already closed.
		rec["state"] = "close"
		// Bump duplicates via async writer if available.
		p.queueIncrement(host, hashStr, 1)
		return rec, plugins.ActionAbortUpdate, nil
	}

	// Watch-field changes trigger re-escalation / flapping.
	changed := false
	for _, w := range watch {
		oldVal, _ := condition.Dig(existing, splitDots(w)...)
		newVal, _ := condition.Dig(rec, splitDots(w)...)
		if !valueEquals(oldVal, newVal) {
			changed = true
			break
		}
	}

	if changed {
		// Decrement the flapping countdown and re-open if necessary.
		fc := flappingCountdown
		if !hasFlap {
			fc = flapping
		}
		fc--
		rec["flapping_countdown"] = fc
		rec["comment_count"] = commentCount + 1
		switch prevState {
		case "close":
			rec["state"] = "open"
		case "ack":
			rec["state"] = "esc"
		default:
			rec["state"] = prevState
		}
		if fc <= 0 {
			// Flapping: discard with a write so the counter / countdown stick.
			return rec, plugins.ActionAbortUpdate, nil
		}
		return rec, plugins.ActionContinue, nil
	}

	if prevState == "close" {
		// Auto re-open without a watch change.
		fc := flappingCountdown
		if !hasFlap {
			fc = flapping
		}
		fc--
		rec["state"] = "open"
		rec["flapping_countdown"] = fc
		rec["comment_count"] = commentCount + 1
		if fc <= 0 {
			return rec, plugins.ActionAbortUpdate, nil
		}
		return rec, plugins.ActionContinue, nil
	}

	if throttling {
		// Throttled duplicate: queue the counter bump but drop notifications.
		p.queueIncrement(host, hashStr, 1)
		rec["state"] = prevState
		return rec, plugins.ActionAbortUpdate, nil
	}

	// Outside the throttle window: pass-through (continue), incrementing the
	// counter, possibly re-escalating from ack.
	if prevState == "ack" {
		rec["state"] = "esc"
	} else {
		rec["state"] = prevState
	}
	rec["comment_count"] = commentCount + 1
	return rec, plugins.ActionContinue, nil
}

// queueIncrement asks the async writer to bump `duplicates` on the matching
// record. If no async writer is wired into the host, the call is a no-op —
// the in-line ActionContinue / ActionAbortUpdate writes still persist the
// updated count we placed on rec.
func (p *Plugin) queueIncrement(host plugins.Host, hash string, delta int64) {
	if host == nil {
		return
	}
	if w, ok := host.(asyncWriterHost); ok {
		if writer := w.AsyncWriter(); writer != nil {
			writer.Increment(recordCollection, "duplicates",
				db.Document{"hash": hash}, delta)
			return
		}
	}
	// Fallback: synchronous IncMany on the driver.
	_, _ = host.DB().IncMany(context.Background(), recordCollection,
		"duplicates", condition.Equals("hash", hash), delta)
}

// asyncWriterHost is an optional interface a Host may satisfy to expose an
// `asyncwriter.Writer`. It avoids forcing the plugins.Host contract to know
// about asyncwriter directly.
type asyncWriterHost interface {
	AsyncWriter() *asyncwriter.Writer
}

// seedDefault writes a `_default` aggregate rule with fingerprint
// [host, source, message] when the collection is empty.
func (p *Plugin) seedDefault(ctx context.Context, host plugins.Host) error {
	if host == nil || host.DB() == nil {
		return nil
	}
	docs, _, err := host.DB().Search(ctx, ruleCollection, condition.Cond{}, db.Page{PerPage: 1})
	if err != nil {
		return err
	}
	if len(docs) > 0 {
		return nil
	}
	_, err = host.DB().Write(ctx, ruleCollection, []db.Document{{
		"name":      "_default",
		"fields":    []string{"host", "source", "message"},
		"condition": []any{},
		"throttle":  defaultThrottle,
		"flapping":  defaultFlapping,
		"enabled":   true,
	}}, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	return err
}

// --- compilation helpers ---

func compileRule(d db.Document) (*compiledRule, error) {
	name, _ := d["name"].(string)
	if name == "" {
		return nil, errors.New("rule has no name")
	}
	enabled := true
	if v, ok := d["enabled"].(bool); ok {
		enabled = v
	}
	c, err := condFromDoc(d["condition"])
	if err != nil {
		return nil, err
	}
	cp, err := condition.Compile(c)
	if err != nil {
		return nil, err
	}
	fields := toStringSlice(d["fields"])
	sort.Strings(fields)
	r := &compiledRule{
		name:     name,
		enabled:  enabled,
		cond:     cp,
		fields:   fields,
		watch:    toStringSlice(d["watch"]),
		throttle: toInt64(d["throttle"], defaultThrottle),
		flapping: toInt64(d["flapping"], defaultFlapping),
	}
	return r, nil
}

// condFromDoc accepts the dual-format condition representation: legacy
// nested-list form or the structured object form.
func condFromDoc(v any) (condition.Cond, error) {
	switch x := v.(type) {
	case nil:
		return condition.Cond{}, nil
	case []any:
		if len(x) == 0 {
			return condition.Cond{}, nil
		}
		return condition.FromList(x)
	case map[string]any:
		// Object form. We marshal then re-decode through UnmarshalJSON so we
		// stay consistent with the canonical JSON parser without duplicating
		// its dispatch logic here.
		var out condition.Cond
		b, err := marshalAny(x)
		if err != nil {
			return condition.Cond{}, err
		}
		if err := out.UnmarshalJSON(b); err != nil {
			return condition.Cond{}, err
		}
		return out, nil
	default:
		return condition.Cond{}, fmt.Errorf("unsupported condition shape %T", v)
	}
}

// --- hashing ---

// computeHash mirrors Python: md5(name + join('field=value', sorted-fields)).
// We use SHA-1 here (cheaper and collision-safe for our cardinality); the on-
// disk hash format is opaque to consumers — only equality matters within a
// single deployment.
func computeHash(name string, fields []string, rec map[string]any) string {
	h := sha1.New() //nolint:gosec
	h.Write([]byte(name))
	for _, f := range fields {
		v, _ := condition.Dig(rec, splitDots(f)...)
		h.Write([]byte(f))
		h.Write([]byte("="))
		_, _ = fmt.Fprint(h, v)
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// defaultHash hashes the entire record (sorted keys), used when no rule
// matched. It groups records whose raw payload is identical.
func defaultHash(rec map[string]any) string {
	h := sha1.New() //nolint:gosec
	keys := make([]string, 0, len(rec))
	for k := range rec {
		if k == "hash" || k == "aggregate" || k == "uid" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("="))
		_, _ = fmt.Fprint(h, rec[k])
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// --- conversion helpers ---

// recordToMap projects a typed Record into the map[string]any shape the
// condition evaluator and the database driver consume.
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
		if _, present := m[k]; !present {
			m[k] = v
		}
	}
	return m
}

// mergeMapIntoRecord pulls plugin-injected fields back onto the typed Record.
// Typed fields are restored; everything else lands in rec.Extra.
func mergeMapIntoRecord(rec *snoozetypes.Record, m map[string]any) {
	if rec.Extra == nil {
		rec.Extra = map[string]any{}
	}
	for k, v := range m {
		switch k {
		case "uid":
			if s, ok := v.(string); ok {
				rec.UID = s
			}
		case "host":
			if s, ok := v.(string); ok {
				rec.Host = s
			}
		case "source":
			if s, ok := v.(string); ok {
				rec.Source = s
			}
		case "process":
			if s, ok := v.(string); ok {
				rec.Process = s
			}
		case "severity":
			if s, ok := v.(string); ok {
				rec.Severity = s
			}
		case "message":
			if s, ok := v.(string); ok {
				rec.Message = s
			}
		case "state":
			if s, ok := v.(string); ok {
				rec.State = s
			}
		case "date_epoch":
			rec.DateEpoch = toInt64(v, rec.DateEpoch)
		case "ttl":
			rec.TTL = toInt64(v, rec.TTL)
		case "plugins":
			// leave rec.Plugins as-is; the pipeline appends to it itself.
		case "timestamp", "tags", "raw", "environment":
			// passthrough untouched in Extra.
			rec.Extra[k] = v
		default:
			rec.Extra[k] = v
		}
	}
}

// --- value helpers ---

func toInt64(v any, fallback int64) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	case uint:
		return int64(n) //nolint:gosec
	case uint32:
		return int64(n)
	case uint64:
		return int64(n) //nolint:gosec
	}
	return fallback
}

func toInt64WithOk(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}

func toStringSlice(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, len(x))
		copy(out, x)
		return out
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

func splitDots(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ".")
}

func valueEquals(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func marshalAny(v any) ([]byte, error) {
	return json.Marshal(v)
}
