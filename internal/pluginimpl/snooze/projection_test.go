package snooze

import (
	"testing"
	"time"

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

// TestProjectDoc_AlwaysOn: a rule with no datetime family is permanent — it
// fires indefinitely, so window_status is "always_on" and there is no
// countdown.
func TestProjectDoc_AlwaysOn(t *testing.T) {
	t.Parallel()
	got := ProjectDoc(db.Document{"name": "perma"}, fixedNow)
	require.Equal(t, "always_on", got["window_status"])
	require.Equal(t, int64(0), got["remaining_seconds"])

	// A document with an empty time_constraints blob (no datetime entries) is
	// also always-on — only weekday/time families do not bound the lifespan.
	got = ProjectDoc(db.Document{
		"name": "weekly",
		"time_constraints": map[string]any{
			"weekdays": []any{map[string]any{"weekdays": []any{1, 2, 3}}},
		},
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
