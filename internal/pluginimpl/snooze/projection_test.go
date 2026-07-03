package snooze

import (
	"testing"
	"time"

	// Embed the zone database so a tz-carrying rule classifies deterministically
	// regardless of the runner's /usr/share/zoneinfo (mirrors the server binary).
	_ "time/tzdata"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
)

// fixedNow is the deterministic clock all projection tests evaluate against.
var fixedNow = time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

// iso renders a time relative to fixedNow as the RFC3339 string the wire
// format uses, so the test fixtures read like real stored documents.
func iso(offset time.Duration) string {
	return fixedNow.Add(offset).Format(time.RFC3339)
}

// TestProjectDoc_AlwaysOn: a rule with no constraint in any family is
// permanent — it fires indefinitely, so window_status is "always_on" and there
// is no countdown.
func TestProjectDoc_AlwaysOn(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{"name": "perma"}, fixedNow)
	require.Equal(t, "always_on", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])

	// A present-but-empty time_constraints blob (no constraints in any family)
	// is likewise always-on.
	got = ProjectDoc(db.Document{
		"name":             "empty",
		"time_constraints": map[string]any{},
	}, fixedNow)
	require.Equal(t, "always_on", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_Active: from in the past, until in the future → active, with
// a positive remaining countdown to the until bound.
func TestProjectDoc_Active(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "active",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-time.Hour), "until": iso(time.Hour)},
			},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(3600), got["remaining_seconds"])
}

// TestProjectDoc_Pending: from in the future → pending, with no countdown
// (remaining_seconds is time left in the *active* window, which has not begun).
func TestProjectDoc_Pending(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "pending",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(time.Hour), "until": iso(2 * time.Hour)},
			},
		},
	}, fixedNow)
	require.Equal(t, "pending", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_Expired: until in the past → expired, no countdown.
func TestProjectDoc_Expired(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "expired",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-2 * time.Hour), "until": iso(-time.Hour)},
			},
		},
	}, fixedNow)
	require.Equal(t, "expired", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_MultipleRanges: one expired range and one active range — the
// active one wins, and remaining is measured against its until.
func TestProjectDoc_MultipleRanges(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "multi",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-3 * time.Hour), "until": iso(-2 * time.Hour)},
				map[string]any{"from": iso(-time.Hour), "until": iso(30 * time.Minute)},
			},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(1800), got["remaining_seconds"])
}

// TestProjectDoc_NoUntil: from in the past, no until → active, open-ended, so
// remaining_seconds is 0 (no upper bound to count down to).
func TestProjectDoc_NoUntil(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "open",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-time.Hour)},
			},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// fixedNow (2026-06-29 12:00 UTC) is a Monday, i.e. time.Weekday == 1. The
// weekday/time cases below lean on that so the fixtures read like real rules.

// TestProjectDoc_WeekdayActive: a weekday-only rule whose list includes today
// is currently suppressing → active. A purely recurring rule never used to be
// anything but always_on (classify ignored the weekday family) — this is the
// #33 regression guard.
func TestProjectDoc_WeekdayActive(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "weekdays-mon-wed",
		"time_constraints": map[string]any{
			"weekdays": []any{map[string]any{"weekdays": []any{1, 2, 3}}},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"], "a recurring window has no countdown")
}

// TestProjectDoc_WeekdayInactive: a weekday-only rule whose list excludes today
// is waiting for its next slot → pending (never expired: it recurs forever).
func TestProjectDoc_WeekdayInactive(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "weekdays-thu-fri",
		"time_constraints": map[string]any{
			"weekdays": []any{map[string]any{"weekdays": []any{4, 5}}},
		},
	}, fixedNow)
	require.Equal(t, "pending", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_TimeOfDayActive: a time-of-day-only window covering now reads
// as active.
func TestProjectDoc_TimeOfDayActive(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "all-day",
		"time_constraints": map[string]any{
			"time": []any{map[string]any{"from": "00:00", "until": "23:59"}},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_TimeOfDayInactive: a time-of-day window that does not cover now
// reads as pending — it will come back around today/tomorrow.
func TestProjectDoc_TimeOfDayInactive(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "afternoon",
		"time_constraints": map[string]any{
			"time": []any{map[string]any{"from": "13:00", "until": "14:00"}},
		},
	}, fixedNow)
	require.Equal(t, "pending", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_DatetimeActiveButWeekdayExcluded: a datetime range that covers
// now but whose weekday family excludes today is NOT suppressing, so the badge
// must not read "active". This is the core #33 bug — classify() used to look at
// the datetime family alone and mislabel this rule active. It reads pending
// because the datetime window is still live (until is in the future).
func TestProjectDoc_DatetimeActiveButWeekdayExcluded(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "window-but-wrong-day",
		"time_constraints": map[string]any{
			"datetime": []any{map[string]any{"from": iso(-time.Hour), "until": iso(time.Hour)}},
			"weekdays": []any{map[string]any{"weekdays": []any{4, 5}}}, // excludes Monday
		},
	}, fixedNow)
	require.Equal(t, "pending", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_DatetimeAndWeekdayBothMatch: datetime range covers now AND the
// weekday family includes today → active, counting down to the datetime until.
func TestProjectDoc_DatetimeAndWeekdayBothMatch(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "window-right-day",
		"time_constraints": map[string]any{
			"datetime": []any{map[string]any{"from": iso(-time.Hour), "until": iso(time.Hour)}},
			"weekdays": []any{map[string]any{"weekdays": []any{1}}}, // Monday
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(3600), got["remaining_seconds"])
}

// TestProjectDoc_ExpiredDatetimeWithRecurring: once the absolute datetime family
// is wholly in the past the rule can never fire again, even with a recurring
// weekday family alongside it → expired.
func TestProjectDoc_ExpiredDatetimeWithRecurring(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "closed-window",
		"time_constraints": map[string]any{
			"datetime": []any{map[string]any{"from": iso(-2 * time.Hour), "until": iso(-time.Hour)}},
			"weekdays": []any{map[string]any{"weekdays": []any{1}}}, // today, but the window closed
		},
	}, fixedNow)
	require.Equal(t, "expired", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_ZonedOffsetInstant is the #31 guard: a datetime bound carrying
// a UTC offset must be classified at the instant that offset denotes, NOT at
// its bare wall clock. The window is 13:00–15:00 at +02:00 (i.e. 11:00–13:00
// UTC); fixedNow is 12:00 UTC, which falls inside it → active. If the offset
// were dropped (the old zone-less-→-UTC trap) the same bounds would read as
// 13:00–15:00 UTC and 12:00 UTC would be pending instead.
func TestProjectDoc_ZonedOffsetInstant(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "paris-window",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{
					"from":  "2026-06-29T13:00:00+02:00", // 11:00 UTC
					"until": "2026-06-29T15:00:00+02:00", // 13:00 UTC
				},
			},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(3600), got["remaining_seconds"], "count down to 13:00 UTC")
}

// TestProjectDoc_ZoneLessDefaultsToUTC documents the deliberate, non-breaking
// default: a zone-less bound is still interpreted as UTC (so rules stored
// before the picker started emitting offsets keep their existing behavior).
// The same 13:00–15:00 wall clock, now WITHOUT an offset, is 13:00–15:00 UTC,
// so fixedNow (12:00 UTC) is ahead of it → pending.
func TestProjectDoc_ZoneLessDefaultsToUTC(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "legacy-window",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": "2026-06-29T13:00", "until": "2026-06-29T15:00"},
			},
		},
	}, fixedNow)
	require.Equal(t, "pending", got["window_status"])
}

// TestProjectDoc_OverlappingActiveRanges: when two active datetime ranges
// overlap, remaining_seconds counts down to the LATEST until, not the first —
// the window stays open until the last active range closes.
func TestProjectDoc_OverlappingActiveRanges(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "overlap",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-time.Hour), "until": iso(30 * time.Minute)},
				map[string]any{"from": iso(-2 * time.Hour), "until": iso(2 * time.Hour)},
			},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(2*3600), got["remaining_seconds"], "count down to the later until")
}

// TestProjectDoc_ActiveOpenEndedOverridesBounded: an active open-ended range
// keeps the window open forever even when a bounded active range sits beside
// it, so there is no countdown.
func TestProjectDoc_ActiveOpenEndedOverridesBounded(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "overlap-open",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-time.Hour), "until": iso(30 * time.Minute)},
				map[string]any{"from": iso(-2 * time.Hour)}, // open-ended
			},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_UntilOnlyRay: a datetime ray bounded only by `until` is active
// while now is at or before it, and expired once now passes it.
func TestProjectDoc_UntilOnlyRay(t *testing.T) {
	t.Parallel()
	active := ProjectDoc(db.Document{
		"name": "until-future",
		"time_constraints": map[string]any{
			"datetime": []any{map[string]any{"until": iso(time.Hour)}},
		},
	}, fixedNow)
	require.Equal(t, "active", active["window_status"])
	require.Equal(t, int64(3600), active["remaining_seconds"])

	expired := ProjectDoc(db.Document{
		"name": "until-past",
		"time_constraints": map[string]any{
			"datetime": []any{map[string]any{"until": iso(-time.Hour)}},
		},
	}, fixedNow)
	require.Equal(t, "expired", expired["window_status"])
}

// TestProjectDoc_FromOnlyFutureRay: a datetime ray bounded only by a future
// `from` has not opened yet → pending (and it never expires — it's open-ended).
func TestProjectDoc_FromOnlyFutureRay(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "from-future",
		"time_constraints": map[string]any{
			"datetime": []any{map[string]any{"from": iso(time.Hour)}},
		},
	}, fixedNow)
	require.Equal(t, "pending", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])
}

// TestProjectDoc_TimeWindowAcrossMidnight exercises the trickiest matcher
// branch — a daily time-of-day window that wraps past midnight (22:00→02:00).
// It is active at 23:00 and inactive (pending, since it recurs) at midday.
func TestProjectDoc_TimeWindowAcrossMidnight(t *testing.T) {
	t.Parallel()
	doc := db.Document{
		"name": "overnight",
		"time_constraints": map[string]any{
			"time": []any{map[string]any{"from": "22:00", "until": "02:00"}},
		},
	}
	night := time.Date(2026, 6, 29, 23, 0, 0, 0, time.UTC)
	require.Equal(t, "active", ProjectDoc(doc, night)["window_status"])

	require.Equal(t, "pending", ProjectDoc(doc, fixedNow)["window_status"]) // 12:00
}

// TestProjectDoc_TimeWindowInNamedZone: the badge honors a named-zone daily
// window. fixedNow is 12:00 UTC = 14:00 Europe/Paris (+02:00 summer); a
// 13:00–18:00 Paris window covers it → active. Read as UTC (no tz) the same
// bounds would be 13:00–18:00 UTC and 12:00 UTC would be pending — so this
// asserts classify() resolves through the group's tz.
func TestProjectDoc_TimeWindowInNamedZone(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "paris-afternoon",
		"time_constraints": map[string]any{
			"tz":   "Europe/Paris",
			"time": []any{map[string]any{"from": "13:00", "until": "18:00"}},
		},
	}, fixedNow)
	require.Equal(t, "active", got["window_status"])
}

// TestProjectDoc_MalformedOmitsStatus: an unparseable time_constraints (here an
// unknown tz) yields NO window_status/remaining_seconds — the pipeline drops
// such a rule, so the badge stays neutral ("—") instead of the old, misleading
// "always_on" that read as "suppressing forever".
func TestProjectDoc_MalformedOmitsStatus(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{
		"name": "broken-tz",
		"time_constraints": map[string]any{
			"tz":   "Mars/Nowhere",
			"time": []any{map[string]any{"from": "09:00", "until": "17:00"}},
		},
	}, fixedNow)
	require.NotContains(t, got, "window_status", "a malformed rule must not assert a lifecycle")
	require.NotContains(t, got, "remaining_seconds")
	require.Equal(t, "broken-tz", got["name"], "other fields are preserved")
}

// TestProjectDoc_ShallowCopy: ProjectDoc must not mutate the input map; it
// returns a shallow copy carrying the two derived fields.
func TestProjectDoc_ShallowCopy(t *testing.T) {
	t.Parallel()
	in := db.Document{
		"name": "active",
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{"from": iso(-time.Hour), "until": iso(time.Hour)},
			},
		},
	}
	got := ProjectDoc(in, fixedNow)
	require.NotContains(t, in, "window_status", "input must not be mutated")
	require.NotContains(t, in, "remaining_seconds", "input must not be mutated")
	require.Equal(t, "active", got["window_status"])
}
