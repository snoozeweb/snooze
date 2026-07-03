package snooze

import (
	"context"
	"time"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/timeconstraints"
)

// windowStatus enumerates the derived lifecycle states a snooze rule can be in
// at a given moment. They mirror Alerta's Blackout.status, plus an explicit
// always_on for rules with no absolute datetime bound (which fire forever).
const (
	statusAlwaysOn = "always_on"
	statusActive   = "active"
	statusPending  = "pending"
	statusExpired  = "expired"
)

// ProjectDoc computes the read-time window_status and remaining_seconds for a
// snooze document and returns a SHALLOW COPY of doc with those two fields
// injected. The input map is never mutated.
//
// Logic (mirrors Alerta's Blackout.status property, extended so the badge
// tracks the SAME predicate the pipeline suppresses on — all populated
// constraint families AND'd, not just the absolute datetime family):
//
//   - No constraint in any family → always_on, remaining 0 (fires forever).
//   - The rule matches now (timeconstraints.Group.Match) → active.
//     remaining_seconds = max(0, seconds until the active datetime range's
//     until) when that range is bounded; 0 for an open-ended active range or a
//     rule active purely via time-of-day/weekday windows.
//   - Not matching now, and the absolute datetime family is wholly in the past
//     (so the rule can never fire again) → expired, remaining 0.
//   - Not matching now otherwise (a future datetime window, or a recurring
//     time-of-day/weekday window waiting for its next slot) → pending,
//     remaining 0.
//
// now is supplied by the caller (the plugin's injected clock) so the result is
// deterministic in tests; ProjectDoc never reads the wall clock itself.
func ProjectDoc(doc db.Document, now time.Time) db.Document {
	status, remaining := classify(doc["time_constraints"], now)

	// Shallow copy so the two derived fields never leak back into the caller's
	// (or the cache's) document. Nested values are shared by reference, which
	// is fine — they are read-only here.
	out := make(db.Document, len(doc)+2)
	for k, v := range doc {
		out[k] = v
	}
	// An empty status means the constraints could not be parsed. The pipeline
	// drops such a rule (it never suppresses), so leave the fields off rather
	// than assert a confident lifecycle — the UI renders a neutral "—" and does
	// not claim the rule is actively suppressing.
	if status != "" {
		out["window_status"] = status
		out["remaining_seconds"] = remaining
	}
	return out
}

// classify parses the time_constraints blob and reduces it to a (status,
// remaining_seconds) pair. A nil/absent constraint blob, or one with no
// constraints in any family, is always_on. Unparseable constraints return an
// empty status so ProjectDoc omits the badge — the pipeline drops the same
// rule, so a neutral "—" is honest where the old always_on wrongly read as
// "suppressing forever".
//
// "active" is decided by timeconstraints.Group.Match — the same time-window
// predicate the pipeline suppresses on — so a rule gated off by a weekday or a
// time-of-day window never reads as active, and a recurring window that has no
// absolute datetime family never reads as always_on. (The pipeline additionally
// requires the rule be enabled and its condition to parse; those are orthogonal
// to the window and out of scope here — the Status column reflects enabled
// separately, and a bad condition is a separate, pre-existing gap.)
func classify(rawTC any, now time.Time) (string, int64) {
	if rawTC == nil {
		return statusAlwaysOn, 0
	}
	g, err := parseTimeConstraints(rawTC)
	if err != nil {
		return "", 0
	}
	// No constraint in any family: the rule fires forever.
	if len(g.DateTime) == 0 && len(g.Time) == 0 && len(g.Weekdays) == 0 {
		return statusAlwaysOn, 0
	}

	// Currently suppressing? Ask the same matcher the pipeline uses.
	if g.Match(now) {
		return statusActive, activeRemaining(g.DateTime, now)
	}

	// Not active now. Only the absolute datetime family can retire a rule for
	// good; recurring time-of-day/weekday windows always come back around, so a
	// rule with a still-live (or absent) datetime family reads as pending.
	if datetimeExhausted(g.DateTime, now) {
		return statusExpired, 0
	}
	return statusPending, 0
}

// activeRemaining returns the countdown to the moment the absolute datetime
// window closes: the LATEST until among the datetime ranges that currently
// cover now (overlapping ranges extend each other, so the first one is not
// necessarily the last to close). A currently-active open-ended range — or an
// active rule with no datetime family at all — never closes on datetime
// grounds, so there is no countdown and it returns 0. This counts the datetime
// bound only; a tighter daily time-of-day window is not reflected
// (remaining_seconds has always been a datetime-family approximation).
func activeRemaining(ranges []timeconstraints.DateTimeConstraint, now time.Time) int64 {
	var (
		latest    time.Time
		haveBound bool
	)
	for _, r := range ranges {
		if !isActiveRange(r, now) {
			continue
		}
		if r.Until == nil {
			// An active open-ended range holds the window open indefinitely,
			// regardless of any bounded ranges alongside it.
			return 0
		}
		if !haveBound || r.Until.After(latest) {
			latest = *r.Until
			haveBound = true
		}
	}
	if !haveBound {
		return 0
	}
	return remainingSeconds(latest, now)
}

// datetimeExhausted reports whether the absolute datetime family exists and is
// wholly in the past — every range is bounded by an until that has already
// elapsed — so the datetime AND-gate can never be satisfied again. A family
// with any open-ended (no-until) or not-yet-elapsed range is still live, as is
// an absent datetime family (a purely recurring rule never expires).
func datetimeExhausted(ranges []timeconstraints.DateTimeConstraint, now time.Time) bool {
	if len(ranges) == 0 {
		return false
	}
	for _, r := range ranges {
		if r.Until == nil || !r.Until.Before(now) {
			return false
		}
	}
	return true
}

// isActiveRange reports whether r covers now: from<=now<=until, or a one-sided
// half-open ray. A range with neither bound matches nothing (consistent with
// timeconstraints.DateTimeConstraint.Match).
func isActiveRange(r timeconstraints.DateTimeConstraint, now time.Time) bool {
	switch {
	case r.From != nil && r.Until != nil:
		return !now.Before(*r.From) && !now.After(*r.Until)
	case r.From == nil && r.Until != nil:
		return !now.After(*r.Until)
	case r.From != nil && r.Until == nil:
		return !now.Before(*r.From)
	default:
		return false
	}
}

// remainingSeconds is max(0, floor(until - now)) in whole seconds.
func remainingSeconds(until, now time.Time) int64 {
	d := until.Sub(now)
	if d < 0 {
		return 0
	}
	return int64(d.Seconds())
}

// Transform implements the plugins.DocTransformer hook. The generic CRUD read
// handlers (list / search / get-one) call it once per fetched document via a
// type assertion, so only the snooze plugin pays the projection cost; every
// other plugin's read path is byte-identical. It delegates to the pure
// ProjectDoc helper using the plugin's injected clock.
func (p *Plugin) Transform(_ context.Context, doc db.Document) db.Document {
	return ProjectDoc(doc, p.now())
}
