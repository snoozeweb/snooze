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
// Logic (mirrors Alerta's Blackout.status property):
//
//   - No datetime family → always_on, remaining 0 (the rule fires forever).
//   - A datetime range covering now (from<=now<=until, or one-sided) → active.
//     remaining_seconds = max(0, seconds until that range's until) when the
//     winning range has an until; 0 for an open-ended (no-until) active range.
//   - Every range strictly in the future (from>now) → pending, remaining 0.
//   - Every range strictly in the past (until<now) → expired, remaining 0.
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
	out["window_status"] = status
	out["remaining_seconds"] = remaining
	return out
}

// classify parses the time_constraints blob and reduces its datetime family to
// a (status, remaining_seconds) pair. A nil/absent/empty datetime family is
// always_on. Unparseable constraints are treated as always_on (fail-safe: a
// malformed rule must not masquerade as expired and silently lose its badge).
func classify(rawTC any, now time.Time) (string, int64) {
	if rawTC == nil {
		return statusAlwaysOn, 0
	}
	g, err := parseTimeConstraints(rawTC)
	if err != nil || len(g.DateTime) == 0 {
		return statusAlwaysOn, 0
	}

	var (
		anyPending bool
		anyExpired bool
	)
	for _, r := range g.DateTime {
		switch {
		case isActiveRange(r, now):
			// First active range wins. remaining counts down to its until,
			// when bounded; an open-ended active range has no countdown.
			if r.Until != nil {
				return statusActive, remainingSeconds(*r.Until, now)
			}
			return statusActive, 0
		case r.From != nil && r.From.After(now):
			anyPending = true
		case r.Until != nil && r.Until.Before(now):
			anyExpired = true
		default:
			// A range with neither bound (matches nothing per the constraint
			// semantics) or any other shape is not active; ignore it.
		}
	}

	// No range is currently active. Prefer pending over expired so a rule with
	// a future window still reads as "scheduled" even if it also has a stale
	// past range alongside it.
	switch {
	case anyPending:
		return statusPending, 0
	case anyExpired:
		return statusExpired, 0
	default:
		return statusAlwaysOn, 0
	}
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
