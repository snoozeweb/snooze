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
// `duplicates` is counted in exactly one place — the merge assignment in
// matchAggregate — because every verdict this plugin returns for a duplicate
// (ActionContinue and ActionAbortUpdate alike) is persisted by the pipeline.
package aggregaterule

import (
	"context"
	"crypto/md5" //nolint:gosec
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/resolutionhold"
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

var _ plugins.DataModel = (*Plugin)(nil)
var _ plugins.WriteTransformer = (*Plugin)(nil)

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the aggregate-rule Processor.
type Plugin struct {
	meta plugins.Metadata

	mu    sync.RWMutex
	rules map[string][]*compiledRule // tenantID → compiled rules
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

// Schema returns the JSON Schema for an aggregate-rule document. `throttle`
// accepts a scalar (seconds) or a {value: seconds} map (with an optional
// "default" key); both are honored by parseThrottle.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"condition": map[string]any{},
			"fields":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"watch":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"throttle": map[string]any{"oneOf": []any{
				map[string]any{"type": "integer"},
				map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer"}},
			}},
			"flapping": map[string]any{"type": "integer"},
			"enabled":  map[string]any{"type": "boolean"},
		},
		"additionalProperties": true,
	}
}

// Validate enforces structural rules. Partial PATCH bodies are tolerated: a
// field is only checked when present.
func (p *Plugin) Validate(obj map[string]any) error {
	if len(obj) == 0 {
		return nil
	}
	if raw, ok := obj["fields"]; ok {
		if len(toStringSlice(raw)) == 0 {
			return errors.New("aggregaterule: fields must not be empty")
		}
	}
	if raw, ok := obj["throttle"]; ok {
		switch x := raw.(type) {
		case nil:
		case map[string]any:
			for k, v := range x {
				n, isNum := toInt64WithOk(v)
				if !isNum {
					return fmt.Errorf("aggregaterule: throttle[%q] must be a number", k)
				}
				if n < 0 {
					return fmt.Errorf("aggregaterule: throttle[%q] must be >= 0", k)
				}
			}
		default:
			n, isNum := toInt64WithOk(raw)
			if !isNum {
				return errors.New("aggregaterule: throttle must be a number or a map of value -> seconds")
			}
			if n < 0 {
				return errors.New("aggregaterule: throttle must be >= 0")
			}
		}
	}
	return nil
}

// TransformWrite blocks creating/updating a rule whose `fields` exactly match
// (as a set) another ENABLED rule's fields — the duplicate-fields footgun that
// fragments one alert's identity across rules. Returning an error aborts the
// CRUD write with HTTP 422. The rule being edited is excluded by uid or name.
//
// Partial PATCH bodies that omit `fields` are not checked (identity unchanged).
func (p *Plugin) TransformWrite(ctx context.Context, doc map[string]any) error {
	raw, present := doc["fields"]
	if !present {
		return nil
	}
	newFields := toStringSlice(raw)
	if len(newFields) == 0 {
		return nil // emptiness is Validate's job
	}
	if en, ok := doc["enabled"].(bool); ok && !en {
		return nil // a disabled rule does not claim identity
	}
	sort.Strings(newFields)

	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		return nil
	}
	selfUID, _ := doc["uid"].(string)
	selfName, _ := doc["name"].(string)

	docs, _, err := host.DB().Search(ctx, ruleCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return nil // best-effort: never block a write on a transient read error
	}
	for _, d := range docs {
		if en, ok := d["enabled"].(bool); ok && !en {
			continue
		}
		if u, _ := d["uid"].(string); u != "" && u == selfUID {
			continue
		}
		if n, _ := d["name"].(string); n != "" && n == selfName {
			continue
		}
		other := toStringSlice(d["fields"])
		sort.Strings(other)
		if slices.Equal(other, newFields) {
			name, _ := d["name"].(string)
			return fmt.Errorf(
				"aggregate fields %v are already used by enabled rule %q — merge them into one rule with a per-value throttle map, or add a discriminator field",
				newFields, name)
		}
	}
	return nil
}

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

// Reload refreshes the in-memory aggregate-rule snapshot for the tenant in ctx.
func (p *Plugin) Reload(ctx context.Context) error {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if host == nil || host.DB() == nil {
		return nil
	}
	tenantID, ok := auth.TenantFrom(ctx)
	if !ok || tenantID == "" {
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
	if p.rules == nil {
		p.rules = make(map[string][]*compiledRule)
	}
	p.rules[tenantID] = rules
	p.mu.Unlock()
	warnDuplicateFields(host, rules)
	return nil
}

// warnDuplicateFields logs once per Reload when two or more enabled rules share
// the same `fields` set — the configuration that fragments an alert's identity
// across rules (and which TransformWrite now blocks for new writes). Existing
// rules are not auto-changed; this points operators at what to merge.
func warnDuplicateFields(host plugins.Host, rules []*compiledRule) {
	if host == nil || host.Logger() == nil {
		return
	}
	byKey := map[string][]string{}
	for _, r := range rules {
		if !r.enabled || len(r.fields) == 0 {
			continue
		}
		key := strings.Join(r.fields, "\x00") // fields are already sorted in compileRule
		byKey[key] = append(byKey[key], r.name)
	}
	for key, names := range byKey {
		if len(names) > 1 {
			host.Logger().Warn("aggregaterule: duplicate aggregate fields across enabled rules",
				"fields", strings.Split(key, "\x00"), "rules", names)
		}
	}
}

// Process implements plugins.Processor. It assigns the record a hash based on
// the first matching rule (or a default-bucket hash) and decides the verdict.
func (p *Plugin) Process(ctx context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	tenantID, _ := auth.TenantFrom(ctx)

	p.mu.RLock()
	rules := p.rules[tenantID]
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

	var afterPersist []func(context.Context)
	out, action, err := p.matchAggregate(ctx, host, recMap, aggrName, throttle, flapping, watchKeys, now(), &afterPersist)
	if err != nil {
		return plugins.Result{Action: plugins.ActionAbort, Record: rec}, err
	}
	mergeMapIntoRecord(&rec, out)
	// The merge only adds keys, so an ownership or first_seen key
	// matchAggregate dropped from the payload (stripOwnership / carryOwnership,
	// a legacy row's first_seen) has to be removed from Extra here, or the
	// payload value would ride on regardless.
	for _, k := range serverOwnedFields {
		if _, kept := out[k]; !kept {
			delete(rec.Extra, k)
		}
	}
	return plugins.Result{Action: action, Record: rec, AfterPersist: afterPersist}, nil
}

// matchAggregate looks up the existing aggregate row by hash and decides
// the verdict. Returns the (possibly mutated) record map and the action; the
// lifecycle comments the verdict implies are appended to afterPersist rather
// than written, so they land only if the pipeline stores the record.
func (p *Plugin) matchAggregate(
	ctx context.Context,
	host plugins.Host,
	rec map[string]any,
	_ string,
	throttle int64,
	flapping int64,
	watch []string,
	now time.Time,
	afterPersist *[]func(context.Context),
) (map[string]any, plugins.Action, error) {
	if host == nil || host.DB() == nil {
		// In tests with no DB the plugin is a no-op pass-through.
		stripOwnership(rec)
		stripResolutionHold(rec)
		rec["duplicates"] = int64(1)
		rec[fieldFirstSeen] = now.Unix()
		newSeverity, _ := rec["severity"].(string)
		stampTrendFields(rec, "", newSeverity)
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
		stripOwnership(rec)
		stripResolutionHold(rec)
		rec["duplicates"] = int64(1)
		rec[fieldFirstSeen] = now.Unix()
		newSeverity, _ := rec["severity"].(string)
		stampTrendFields(rec, "", newSeverity)
		return rec, plugins.ActionContinue, nil
	}

	// Carry forward server-injected fields the incoming alert can't supply.
	// Snooze 1.x merged the entire existing record onto the incoming one
	// (src/snooze/plugins/core/aggregaterule/plugin.py:73,
	// `dict(aggregate.items() + record.items())`). The Go port keeps the
	// identity handling explicit below, but must still ferry the notifier
	// handle fields forward — `notify_ref_<action>` and its legacy
	// `response_<action>` predecessor (see plugins.IsNotifyRefField). They are
	// stamped by a previous notification (a notifier's StoreNotifyRef, or
	// webhook's inject_response) and never appear on the incoming alert, so
	// without this a notifier can't find the JIRA issue / chat thread it
	// already created and would create a second one on every re-escalation.
	// The escalation context is ferried for the same reason: a duplicate
	// occurrence of an already-escalated alert must not look like a first
	// delivery to a notifier, or it would create a second ticket for an
	// incident it is already tracking.
	//
	// The suppression attribution (`snoozed`) rides forward on the same
	// principle, and this plugin's involvement with it ENDS there. It belongs
	// to the snooze plugin (plugins.SuppressionOwner), which decides on every
	// occurrence whether to keep, replace or clear it. This plugin used to
	// delete it from the record collection whenever its own verdict was
	// ActionContinue, predicting that snooze was about to re-decide — a
	// prediction that is false for a `close` (snooze passes those through
	// untouched), so a recovery wiped the attribution and the next occurrence,
	// held by the throttle below, never reached snooze to rebuild it. Carrying
	// the value forward and letting the owner decide replaces that guess.
	//
	// Incoming keys win on collision — the alert payload stays authoritative
	// for everything it does carry.
	for k, v := range existing {
		if !plugins.IsNotifyRefField(k) && !carryForwardFields[k] {
			continue
		}
		if _, ok := rec[k]; !ok {
			rec[k] = v
		}
	}
	// Ownership (internal/ownership) rides forward too, so a duplicate and
	// the notifiers it reaches see who is working on the alert — the merge
	// write would keep the stored keys anyway, but the in-flight record would
	// not carry them. Unlike the fields above the STORED value wins on
	// collision: ownership is only ever set by an operator action, and an
	// alert payload that happens to carry an `owner` key (a monitoring label
	// naming a team, say) must not reassign an alert somebody has taken. The
	// transitions below that drop the owner overwrite these with a clear.
	carryOwnership(rec, existing)
	// The resolution hold is server state on the same terms: the stored value
	// wins, so an alert payload can neither arm nor lift a hold.
	carryResolutionHold(rec, existing)
	// first_seen: the stored value, always — or none, for a row that predates
	// the field (inventing a start would claim a history we do not have).
	if v, ok := existing[fieldFirstSeen]; ok {
		rec[fieldFirstSeen] = v
	} else {
		delete(rec, fieldFirstSeen)
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

	// Derive the previous_severity / trend_indication pair stamped on every
	// non-error return path below. trendCmp < 0 means severity ROSE (more
	// severe), which the throttled-duplicate path uses to break through the
	// throttle so an escalation is never silently dropped.
	prevSeverity, _ := existing["severity"].(string)
	newSeverity, _ := rec["severity"].(string)
	trendCmp := snoozetypes.CompareSeverity(newSeverity, prevSeverity)
	stampTrendFields(rec, prevSeverity, newSeverity)

	incomingState, _ := rec["state"].(string)
	incomingDateEpoch := toInt64(rec["date_epoch"], now.Unix())
	rec["uid"] = prevUID
	// The one and only `duplicates` bump. Every return path below is written
	// by the pipeline — ActionAbortUpdate persists too, it just doesn't stamp
	// date_epoch — so this assignment counts the occurrence exactly once
	// whatever the verdict. A second mechanism used to bump the counter again
	// on the two abort paths (see the git history of queueIncrement) and
	// double-counted every throttled duplicate.
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
			// Terminal: the next occurrence is a genuinely new incident, so
			// clear the escalation lifecycle rather than letting the next
			// delivery inherit a count (and comment on a resolved ticket).
			resetEscalation(rec)
			// An automatic close is not a claim that anybody fixed anything.
			clearResolutionHold(rec)
			reason := fmt.Sprintf("Severity %s => %s", prevSeverity, newSeverity)
			p.queueAutoComment(afterPersist, host, prevUID, "close", "Auto closed: "+reason, now)
			// Tell the notifiers that opened a ticket for this alert, once the
			// close has been stored (plugins.CloseNotifier).
			if afterPersist != nil && prevUID != "" {
				*afterPersist = append(*afterPersist, func(ctx context.Context) {
					plugins.DispatchClose(ctx, host, prevUID, plugins.CloseEvent{Reason: reason, At: now.UTC()})
				})
			}
			return rec, plugins.ActionContinue, nil
		}
		// Already closed. If an operator's resolution hold is running, the
		// source has now caught up with the fix: end the hold quietly, so a
		// later occurrence — a genuinely new incident — re-opens as usual.
		rec["state"] = "close"
		if resolutionhold.Until(existing) > 0 {
			clearResolutionHold(rec)
		}
		return rec, plugins.ActionAbortUpdate, nil
	}

	// Resolution hold: an operator closed this alert as fixed (verdict
	// resolved / self_resolved) and the source is still firing — typically a
	// rule over a look-back window that has not caught up with the fix yet.
	// Keep it closed: no re-open, no owner clear, no notification. The
	// occurrence is still counted (the duplicates bump above) and persisted
	// (ActionAbortUpdate), and the timeline says so once per hold. Checked
	// before the watch-field and re-open paths below, both of which would
	// otherwise re-open a closed aggregate. A severity RISE is not held: the
	// alert got worse than what was fixed, which is news.
	if prevState == "close" && trendCmp >= 0 {
		if until, held := resolutionhold.Held(existing, now.Unix()); held {
			rec["state"] = "close"
			if !resolutionhold.Noted(existing) {
				rec[resolutionhold.FieldNoted] = true
				rec["comment_count"] = commentCount + 1
				p.queueAutoComment(afterPersist, host, prevUID, "comment",
					"Source still firing after resolution — kept closed (hold until "+
						time.Unix(until, 0).UTC().Format(time.RFC3339)+")", now)
			}
			return rec, plugins.ActionAbortUpdate, nil
		}
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
		fc := nextFlappingCountdown(flappingCountdown, hasFlap, flapping, throttling)
		rec["flapping_countdown"] = fc
		rec["comment_count"] = commentCount + 1
		fields := strings.Join(changedFields, ", ")
		var ctype, msg string
		switch prevState {
		case "close":
			rec["state"] = "open"
			clearOwnership(rec, existing)
			clearResolutionHold(rec)
			ctype, msg = "open", "Auto re-opened from watchlist: "+fields
		case "ack":
			rec["state"] = "esc"
			clearOwnership(rec, existing)
			ctype, msg = "esc", "Auto re-escalated from watchlist: "+fields
			// Watchlist auto-re-escalation is an escalation producer like the
			// escalate-timeout sweep: stamp the context so the notifiers this
			// record is about to reach update the ticket / thread they already
			// created instead of opening a second one.
			rec["escalation_count"] = int(toInt64(existing["escalation_count"], 0)) + 1
			rec["escalated_at"] = now.Unix()
			rec["escalation_reason"] = "watchlist"
		default:
			rec["state"] = prevState
			ctype, msg = "comment", "New escalation from watchlist: "+fields
		}
		if fc <= 0 {
			// Flapping: discard with a write so the counter / countdown stick.
			msg += "\n" + flappingNote(throttle, now.Unix()-prevDate)
			p.queueAutoComment(afterPersist, host, prevUID, ctype, msg, now)
			return rec, plugins.ActionAbortUpdate, nil
		}
		p.queueAutoComment(afterPersist, host, prevUID, ctype, msg, now)
		return rec, plugins.ActionContinue, nil
	}

	if prevState == "close" {
		// Auto re-open without a watch change.
		fc := nextFlappingCountdown(flappingCountdown, hasFlap, flapping, throttling)
		rec["state"] = "open"
		clearOwnership(rec, existing)
		clearResolutionHold(rec)
		rec["flapping_countdown"] = fc
		rec["comment_count"] = commentCount + 1
		msg := "Auto re-opened"
		if fc <= 0 {
			msg += "\n" + flappingNote(throttle, now.Unix()-prevDate)
			p.queueAutoComment(afterPersist, host, prevUID, "open", msg, now)
			return rec, plugins.ActionAbortUpdate, nil
		}
		p.queueAutoComment(afterPersist, host, prevUID, "open", msg, now)
		return rec, plugins.ActionContinue, nil
	}

	if throttling {
		if trendCmp < 0 {
			// Severity ROSE inside the throttle window — break through the
			// throttle so the downstream notification sees the escalation
			// instead of silently swallowing the duplicate. Only moreSevere
			// bypasses; lessSevere / noChange stay throttled below.
			ctype := "esc"
			if prevState == "ack" {
				rec["state"] = "esc"
				clearOwnership(rec, existing)
			} else {
				rec["state"] = prevState
			}
			rec["comment_count"] = commentCount + 1
			p.queueAutoComment(afterPersist, host, prevUID, ctype,
				fmt.Sprintf("Severity escalated: %s => %s (throttle bypassed)", prevSeverity, newSeverity), now)
			return rec, plugins.ActionContinue, nil
		}
		// Throttled duplicate: counted above, notifications dropped.
		ruleName, _ := rec["aggregate"].(string)
		plugins.RecordStat(ctx, host, incomingDateEpoch, "alert_throttled", map[string]string{"name": ruleName}, 1)
		rec["state"] = prevState
		return rec, plugins.ActionAbortUpdate, nil
	}

	// Outside the throttle window: pass-through (continue), incrementing the
	// counter, possibly re-escalating from ack.
	ctype := "comment"
	if prevState == "ack" {
		rec["state"] = "esc"
		clearOwnership(rec, existing)
		ctype = "esc"
	} else {
		rec["state"] = prevState
	}
	rec["comment_count"] = commentCount + 1
	p.queueAutoComment(afterPersist, host, prevUID, ctype, "New escalation", now)
	return rec, plugins.ActionContinue, nil
}

// nextFlappingCountdown spends one unit of the aggregate's anti-flapping
// budget and returns the remainder. A remainder of 0 or less means this hit is
// held back (see the callers).
//
// The budget is per throttle window, not per record lifetime: "only 3
// subsequent hits can be notified until the throttle period ends"
// (docs/content/general/aggregaterules.md). So it refills whenever the
// aggregate has been quiet for a full throttle window — !throttling, i.e. the
// last pass-through was longer ago than the throttle — and on a record that
// has no countdown yet.
//
// Without the refill the countdown only ever fell, so after `flapping`
// watch-field changes in the aggregate's whole life EVERY later re-open or
// re-escalation was dropped as flapping, however many quiet hours sat in
// between. Two costs, both seen in production on a nightly K8s alert whose
// countdown had reached -9: the meaningful transition (ok => critical) was
// swallowed, and because that path aborts without stamping date_epoch, the
// throttle clock was not restarted either — so the next plain repeat, 14
// seconds later, sailed through as a fresh "New escalation".
//
// The result is clamped at 0: a negative countdown carries no more
// information than 0 (both mean "budget spent") and 0 refills identically.
func nextFlappingCountdown(countdown int64, hasCountdown bool, budget int64, throttling bool) int64 {
	fc := countdown
	if !hasCountdown || !throttling {
		fc = budget
	}
	fc--
	if fc < 0 {
		fc = 0
	}
	return fc
}

// queueAutoComment queues an automatic lifecycle comment for the `comment`
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
// The comment is QUEUED on plugins.Result.AfterPersist, not written here: the
// comment_count bump rides on the record, which a plugin behind this one (a
// snooze discard) can still drop, and a comment written up front then
// narrated a transition that was never stored — "Auto re-opened" on a row
// that stayed closed. The pipeline runs the effect only after the write.
//
// The write goes straight to the driver, not through POST /api/v1/comment, so
// the comment plugin's AfterCreate hook does NOT fire and does NOT double-count
// (the caller already bumps comment_count on the record). Failures are
// best-effort logged: a missing timeline entry must never drop the alert.
func (p *Plugin) queueAutoComment(afterPersist *[]func(context.Context), host plugins.Host, recordUID, ctype, message string, now time.Time) {
	if afterPersist == nil || host == nil || host.DB() == nil || recordUID == "" || message == "" {
		return
	}
	doc := db.Document{
		"record_uid": recordUID,
		"type":       ctype,
		"message":    message,
		"date_epoch": now.Unix(),
		"auto":       true,
	}
	*afterPersist = append(*afterPersist, func(ctx context.Context) {
		if _, err := host.DB().Write(ctx, "comment", []db.Document{doc}, db.WriteOptions{UpdateTime: true}); err != nil {
			if host.Logger() != nil {
				host.Logger().Warn("aggregaterule: write auto comment",
					"record_uid", recordUID, "type", ctype, "error", err)
			}
		}
	})
}

// trendString maps the return value of snoozetypes.CompareSeverity to the
// Alerta TrendIndication string labels.
func trendString(cmp int) string {
	switch {
	case cmp < 0:
		return "moreSevere"
	case cmp > 0:
		return "lessSevere"
	default:
		return "noChange"
	}
}

// carryForwardFields are the server-stamped fields that must ride forward from
// the stored aggregate onto a duplicate occurrence, alongside the notifier
// handle fields. The incoming alert never carries them, so without this a
// duplicate of an escalated alert reaches the notifiers as a first delivery,
// and a duplicate of a silenced alert reaches the snooze plugin looking like
// an alert that was never silenced.
var carryForwardFields = map[string]bool{
	// Owned by the snooze plugin; ferried, never interpreted here.
	"snoozed":           true,
	"snooze_released":   true,
	"escalation_count":  true,
	"escalated_at":      true,
	"escalation_reason": true,
	"escalation_actor":  true,
}

// carryOwnership copies the stored ownership keys onto the incoming record,
// overwriting any the alert payload carried (see the call site for why the
// stored value wins here, unlike carryForwardFields).
//
// A key the stored row lacks is dropped from the payload rather than left in:
// a row that predates ownership (or `migrate owners`) must not become owned by
// whatever the alert happens to send.
func carryOwnership(rec map[string]any, existing db.Document) {
	for _, k := range ownership.Fields {
		if v, ok := existing[k]; ok {
			rec[k] = v
		} else {
			delete(rec, k)
		}
	}
}

// fieldFirstSeen is the epoch the aggregate was created — the first time this
// alert was seen. Server-owned: stamped once here, never taken from a payload.
const fieldFirstSeen = "first_seen"

// serverOwnedFields are the record keys an alert payload can never set: the
// ownership keys and first_seen.
var serverOwnedFields = append(append(append([]string{}, ownership.Fields...), resolutionhold.Fields...), fieldFirstSeen)

// stripResolutionHold drops any resolution-hold key an alert payload carries on
// its first occurrence: only an operator's close arms a hold.
func stripResolutionHold(rec map[string]any) {
	for _, k := range resolutionhold.Fields {
		delete(rec, k)
	}
}

// carryResolutionHold copies the stored resolution-hold keys onto a duplicate,
// overwriting whatever the payload carried (see carryOwnership for the same
// stored-wins rule). A key the stored row lacks is dropped from the payload.
func carryResolutionHold(rec map[string]any, existing db.Document) {
	for _, k := range resolutionhold.Fields {
		if v, ok := existing[k]; ok {
			rec[k] = v
		} else {
			delete(rec, k)
		}
	}
}

// clearResolutionHold ends any hold on an aggregate this occurrence closes
// automatically or re-opens. Explicit zeros, never an unset — the pipeline's
// merge write would leave an absent key untouched.
func clearResolutionHold(rec map[string]any) {
	for k, v := range resolutionhold.Clear() {
		rec[k] = v
	}
}

// stripOwnership drops any ownership key an alert payload carries on its first
// occurrence. Only an operator action makes somebody the owner, so a monitoring
// label that happens to be called `owner` must not arrive as an owned alert.
func stripOwnership(rec map[string]any) {
	for _, k := range ownership.Fields {
		delete(rec, k)
	}
}

// clearOwnership drops the owner of an aggregate this occurrence re-opens or
// re-escalates (the alert needs somebody's attention again), keeping it as the
// previous-owner ghost. It is computed from existing — the row as stored — not
// from rec, which already carries the stored keys forward.
//
// Like resetEscalation the clear is EXPLICIT ("" / 0): the keys ride through
// mergeMapIntoRecord into Record.Extra and the pipeline's merge write leaves an
// absent key untouched, so an unset would leave the old owner in place.
func clearOwnership(rec map[string]any, existing db.Document) {
	for k, v := range ownership.Clear(existing) {
		rec[k] = v
	}
}

// resetEscalation clears the escalation lifecycle on a record that is being
// closed, so its next occurrence starts from a first delivery. Mirrors the
// comment plugin's close transition.
//
// The zeros are written EXPLICITLY rather than left absent: these keys ride
// through mergeMapIntoRecord into Record.Extra, and the pipeline's projector
// elides zero-valued typed fields — so an absent key would leave the stored
// count untouched on a merge write, and the next occurrence would inherit it.
func resetEscalation(rec map[string]any) {
	rec["escalation_count"] = 0
	rec["escalated_at"] = int64(0)
	rec["escalation_reason"] = ""
	rec["escalation_actor"] = ""
}

// stampTrendFields records the derived previous_severity / trend_indication
// pair on the outgoing record. `prev` is the severity on the existing aggregate
// (empty on first occurrence) and `next` the incoming severity. It is called on
// every non-error return path so dashboard sort/badge consumers can read both
// fields regardless of the pipeline verdict.
func stampTrendFields(rec map[string]any, prev, next string) {
	rec["previous_severity"] = prev
	rec["trend_indication"] = trendString(snoozetypes.CompareSeverity(next, prev))
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
