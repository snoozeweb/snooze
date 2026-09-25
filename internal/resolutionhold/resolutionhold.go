// Package resolutionhold holds the primitives behind the resolution hold: the
// window after an operator closes an alert as fixed during which a re-fire of
// the same aggregate keeps it closed instead of re-opening and re-escalating it.
//
// The case it exists for is a fix that outruns its monitoring. An operator
// fixes the problem, records the verdict as `resolved` and closes the alert;
// the rule that raised it looks back over a window (a range query over the last
// hour, say), so the source keeps firing for a while after the fix. Without the
// hold, the first such occurrence re-opens the alert, clears its owner and
// escalates it again — paging on-call for a problem that is already fixed —
// and the source's eventual recovery closes it as if nobody had touched it.
//
// Two UNTYPED record keys carry the state (they ride in Record.Extra):
//
//	resolution_hold_until  epoch seconds the hold ends; 0 = no hold
//	resolution_hold_noted  whether the timeline already says the alert is held
//
// The comment plugin stamps the deadline on a HUMAN close (a close comment with
// a user); an automatic close, a re-open and an escalation write 0. The
// aggregaterule processor reads it on a re-fire and releases it (writes 0) when
// the source reports recovery, so a later, genuinely new occurrence re-opens
// normally. As with ownership, clears are explicit zeros, never unsets: the
// pipeline's merge write leaves an absent key untouched.
//
// Holding also needs the verdict: only an alert whose agentic analysis says
// `resolved` or `self_resolved` is held. A close without that verdict is a
// close like any other — the operator has not claimed the problem is fixed.
//
// Everything here is a pure function over map[string]any: no driver, no clock.
package resolutionhold

import (
	"encoding/json"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// The resolution-hold keys on a record document.
const (
	FieldUntil = "resolution_hold_until"
	FieldNoted = "resolution_hold_noted"
)

// Fields lists every resolution-hold key, for callers that ferry or strip them
// as a set.
var Fields = []string{FieldUntil, FieldNoted}

// Start returns the patch a human close writes: a hold ending hold after now,
// or none (0) when hold is not positive. The noted flag is reset so the next
// held occurrence narrates itself once.
func Start(now int64, hold time.Duration) map[string]any {
	until := int64(0)
	if hold > 0 {
		until = now + int64(hold.Seconds())
	}
	return map[string]any{FieldUntil: until, FieldNoted: false}
}

// Clear returns the patch that ends any hold: an automatic close, a re-open, an
// escalation, or the source reporting recovery during the hold.
func Clear() map[string]any {
	return map[string]any{FieldUntil: int64(0), FieldNoted: false}
}

// Until returns the stored hold deadline, 0 when there is none.
func Until(doc map[string]any) int64 {
	return asInt64(doc[FieldUntil])
}

// Noted reports whether the timeline already carries the held-occurrence note.
func Noted(doc map[string]any) bool {
	b, _ := doc[FieldNoted].(bool)
	return b
}

// Held reports whether a re-fire at now of the closed aggregate existing must
// keep it closed, and until when. It requires an unexpired deadline AND a
// resolved / self_resolved verdict on the stored analysis.
func Held(existing map[string]any, now int64) (until int64, held bool) {
	until = Until(existing)
	if until <= 0 || now >= until {
		return until, false
	}
	switch Verdict(existing) {
	case snoozetypes.PlanResolved, snoozetypes.PlanSelfResolved:
		return until, true
	}
	return until, false
}

// Verdict returns agentic.remediation_plan.status of the stored record, "" when
// it has no analysis or the analysis states no verdict.
func Verdict(doc map[string]any) string {
	v, ok := condition.Dig(doc, "agentic", "remediation_plan", "status")
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// asInt64 tolerates the int / int64 / float64 / json.Number shapes the Mongo,
// Postgres and SQLite drivers decode numbers into.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
