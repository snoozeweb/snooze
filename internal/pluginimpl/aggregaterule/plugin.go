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
	"crypto/md5" //nolint:gosec
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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
	throttle throttleSpec
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
		throttle = r.throttle.resolve(recMap, r.watch)
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
) (outRec map[string]any, outAction plugins.Action, outErr error) {
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

	// Carry forward server-injected fields the incoming alert can't supply.
	// Snooze 1.x merged the entire existing record onto the incoming one
	// (src/snooze/plugins/core/aggregaterule/plugin.py:73,
	// `dict(aggregate.items() + record.items())`). The Go port keeps the
	// identity handling explicit below, but must still ferry `response_<action>`
	// fields forward: they are stamped by a previous notification's
	// inject_response and never appear on the incoming alert, so without this
	// the notification/webhook can't read the recorded Teams message ids to
	// thread a follow-up reply. Incoming keys win on collision — the alert
	// payload stays authoritative for everything it does carry.
	for k, v := range existing {
		if !strings.HasPrefix(k, "response_") {
			continue
		}
		if _, ok := rec[k]; !ok {
			rec[k] = v
		}
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

	// If this record carried a stale `snoozed` attribution and is about to
	// continue to the snooze plugin (ActionContinue → next in the pipeline),
	// clear it so snooze re-evaluates the *current* record and re-asserts
	// `snoozed` only if it still matches a filter. Without this, an alert
	// snoozed as a warning keeps `snoozed` after escalating to emergency and
	// never returns to the Alerts tab. Paths that abort (throttled, flapping,
	// already-closed) never reach snooze, so their attribution is left intact.
	// The merge write at pipeline end cannot remove a key, hence the explicit
	// UnsetFields against the existing row.
	if _, hadSnoozed := existing["snoozed"]; hadSnoozed && prevUID != "" {
		defer func() {
			if outErr != nil || outAction != plugins.ActionContinue {
				return
			}
			if _, err := host.DB().UnsetFields(ctx, recordCollection,
				[]string{"snoozed"}, condition.Equals("uid", prevUID)); err != nil && host.Logger() != nil {
				host.Logger().Warn("aggregaterule: clear stale snoozed",
					"uid", prevUID, "error", err)
			}
		}()
	}

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
			prevSeverity, _ := existing["severity"].(string)
			newSeverity, _ := rec["severity"].(string)
			p.writeAutoComment(ctx, host, prevUID, "close",
				fmt.Sprintf("Auto closed: Severity %s => %s", prevSeverity, newSeverity), now)
			return rec, plugins.ActionContinue, nil
		}
		// Already closed.
		rec["state"] = "close"
		// Bump duplicates via async writer if available.
		p.queueIncrement(host, hashStr, 1)
		return rec, plugins.ActionAbortUpdate, nil
	}

	// Watch-field changes trigger re-escalation / flapping.
	var changedFields []string
	for _, w := range watch {
		oldVal, _ := condition.Dig(existing, splitDots(w)...)
		newVal, _ := condition.Dig(rec, splitDots(w)...)
		if !valueEquals(oldVal, newVal) {
			changedFields = append(changedFields, fmt.Sprintf("%s (%v => %v)", w, oldVal, newVal))
		}
	}

	if len(changedFields) > 0 {
		// Decrement the flapping countdown and re-open if necessary.
		fc := flappingCountdown
		if !hasFlap {
			fc = flapping
		}
		fc--
		rec["flapping_countdown"] = fc
		rec["comment_count"] = commentCount + 1
		fields := strings.Join(changedFields, ", ")
		var ctype, msg string
		switch prevState {
		case "close":
			rec["state"] = "open"
			ctype, msg = "open", "Auto re-opened from watchlist: "+fields
		case "ack":
			rec["state"] = "esc"
			ctype, msg = "esc", "Auto re-escalated from watchlist: "+fields
		default:
			rec["state"] = prevState
			ctype, msg = "comment", "New escalation from watchlist: "+fields
		}
		if fc <= 0 {
			// Flapping: discard with a write so the counter / countdown stick.
			msg += "\n" + flappingNote(throttle, now.Unix()-prevDate)
			p.writeAutoComment(ctx, host, prevUID, ctype, msg, now)
			return rec, plugins.ActionAbortUpdate, nil
		}
		p.writeAutoComment(ctx, host, prevUID, ctype, msg, now)
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
		msg := "Auto re-opened"
		if fc <= 0 {
			msg += "\n" + flappingNote(throttle, now.Unix()-prevDate)
			p.writeAutoComment(ctx, host, prevUID, "open", msg, now)
			return rec, plugins.ActionAbortUpdate, nil
		}
		p.writeAutoComment(ctx, host, prevUID, "open", msg, now)
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
	ctype := "comment"
	if prevState == "ack" {
		rec["state"] = "esc"
		ctype = "esc"
	} else {
		rec["state"] = prevState
	}
	rec["comment_count"] = commentCount + 1
	p.writeAutoComment(ctx, host, prevUID, ctype, "New escalation", now)
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

// writeAutoComment persists an automatic lifecycle comment onto the `comment`
// collection so a record's timeline reflects the state transitions the
// aggregate pipeline performs (close, re-open, re-escalation, watch-field
// changes).
//
// Snooze 1.x wrote such a comment on every comment_count bump
// (src/snooze/plugins/core/aggregaterule/plugin.py:`self.db.write('comment', …)`).
// The Go port kept the counter increment but dropped the write, so
// comment_count inflated unbounded while the timeline — which reads real
// comment docs by record_uid — stayed empty. This restores the 1:1 invariant.
//
// The write goes straight to the driver, not through POST /api/v1/comment, so
// the comment plugin's AfterCreate hook does NOT fire and does NOT double-count
// (the caller already bumps comment_count on the record). Failures are
// best-effort logged: a missing timeline entry must never drop the alert.
func (p *Plugin) writeAutoComment(ctx context.Context, host plugins.Host, recordUID, ctype, message string, now time.Time) {
	if host == nil || host.DB() == nil || recordUID == "" || message == "" {
		return
	}
	doc := db.Document{
		"record_uid": recordUID,
		"type":       ctype,
		"message":    message,
		"date_epoch": now.Unix(),
		"auto":       true,
	}
	if _, err := host.DB().Write(ctx, "comment", []db.Document{doc}, db.WriteOptions{UpdateTime: true}); err != nil {
		if host.Logger() != nil {
			host.Logger().Warn("aggregaterule: write auto comment",
				"record_uid", recordUID, "type", ctype, "error", err)
		}
	}
}

// flappingNote renders the "stopped notifications until throttle expires" line
// Snooze 1.x appended to a comment when the flapping countdown hit zero.
func flappingNote(throttle, elapsed int64) string {
	left := throttle - elapsed
	if left < 0 {
		left = 0
	}
	return fmt.Sprintf("Flapping detected. Stopped notifications until throttle expires (%ds left)", left)
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
		throttle: parseThrottle(d["throttle"]),
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

// computeHash builds the aggregate identity: md5 of the rule name followed by
// each sorted `field=value|` pair (note the `|` separator and the trailing `|`).
//
// This is NOT byte-compatible with the Snooze 1.x Python hash, which joined the
// pairs with `.` and no trailing separator — verified by reproduction: the same
// record+rule yields a different digest under each scheme. MD5 is retained as
// the digest function, but the differing serialization means open aggregates do
// NOT keep their hash across a Python→Go migration; they re-form on the first
// post-migration occurrence. Keep this serialization stable from here on:
// changing it re-forks every currently-open aggregate.
func computeHash(name string, fields []string, rec map[string]any) string {
	h := md5.New() //nolint:gosec
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
	h := md5.New() //nolint:gosec
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
		case "hash":
			if s, ok := v.(string); ok {
				rec.Hash = s
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

// throttleSpec is a rule's throttle policy. A scalar throttle compiles to
// {def, nil}; a map compiles to per-value overrides plus a default. resolve
// matches the rule's watch-field values (in order) against the overrides and
// returns the first hit, else def. This generalizes per-severity throttling to
// any watched field.
type throttleSpec struct {
	def     int64
	byValue map[string]int64
}

// parseThrottle accepts the stored `throttle` field in either form:
//   - a scalar number  -> applies always
//   - a map[string]any -> {value: seconds, …} plus an optional "default" key
func parseThrottle(v any) throttleSpec {
	switch x := v.(type) {
	case nil:
		return throttleSpec{def: defaultThrottle}
	case map[string]any:
		ts := throttleSpec{def: defaultThrottle, byValue: make(map[string]int64, len(x))}
		for k, raw := range x {
			n := toInt64(raw, defaultThrottle)
			if k == "default" {
				ts.def = n
				continue
			}
			ts.byValue[k] = n
		}
		return ts
	default:
		return throttleSpec{def: toInt64(v, defaultThrottle)}
	}
}

// resolve picks the throttle (seconds) for rec: the first watched field whose
// value is an override key wins; otherwise def.
// Values are compared by their string form (fmt.Sprint), so map keys must be strings (as JSON/YAML object keys always are).
func (t throttleSpec) resolve(rec map[string]any, watch []string) int64 {
	if len(t.byValue) > 0 {
		for _, w := range watch {
			v, _ := condition.Dig(rec, splitDots(w)...)
			if secs, ok := t.byValue[fmt.Sprint(v)]; ok {
				return secs
			}
		}
	}
	return t.def
}

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
