package aggregaterule

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ownedAs stamps state + an owner onto the stored aggregate, standing in for
// an operator's ack (or assign) between two occurrences.
func ownedAs(t *testing.T, host *testHost, hash, state, owner string) {
	t.Helper()
	patch := db.Document{"state": state}
	for k, v := range ownership.Take(owner, "ldap", 111) {
		patch[k] = v
	}
	_, err := host.driver.SetFields(tctx(), recordCollection, patch, condition.Equals("hash", hash))
	require.NoError(t, err)
}

// requireCleared asserts that rec — the in-flight record handed to the rest of
// the pipeline and the notifiers — carries the explicit-empty clear with the
// previous owner kept as the ghost.
func requireCleared(t *testing.T, rec snoozetypes.Record, prevOwner string) {
	t.Helper()
	require.Equal(t, "", rec.Extra[ownership.FieldOwner], "cleared with an explicit empty, never unset")
	require.Equal(t, "", rec.Extra[ownership.FieldOwnerMethod])
	require.Equal(t, int64(0), rec.Extra[ownership.FieldOwnerSince])
	require.Equal(t, prevOwner, rec.Extra[ownership.FieldPreviousOwner])
	require.Equal(t, "ldap", rec.Extra[ownership.FieldPreviousOwnerMethod])
}

// requireOwned asserts the owner rode forward untouched.
func requireOwned(t *testing.T, rec snoozetypes.Record, owner string) {
	t.Helper()
	require.Equal(t, owner, rec.Extra[ownership.FieldOwner])
	require.Equal(t, "ldap", rec.Extra[ownership.FieldOwnerMethod])
	require.Equal(t, int64(111), toInt64(rec.Extra[ownership.FieldOwnerSince], -1))
}

// Every automatic re-open or re-escalation clears the owner; see the
// "Transition table" of the alert-ownership design.
func TestAggregate_OwnershipClearedOnWatchlistTransitions(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ from, to string }{
		"close re-opened":  {"close", "open"},
		"ack re-escalated": {"ack", "esc"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			host := newTestHost(t)
			writeRule(t, host, db.Document{
				"name": "AggOwnW", "condition": []any{"=", "a", "w"},
				"fields": []string{"a"}, "watch": []string{"c"},
				"throttle": int64(900), "flapping": int64(3),
			})
			p := freshPlugin(t, host)

			first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "w", "c": "1"}})
			ownedAs(t, host, first.Hash, tc.from, "alice")

			out, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "w", "c": "2"}})
			require.Equal(t, tc.to, out.State)
			requireCleared(t, out, "alice")

			stored := recordsByAggregate(t, host, "AggOwnW")
			require.Len(t, stored, 1)
			require.Equal(t, "", stored[0][ownership.FieldOwner])
			require.Equal(t, "alice", stored[0][ownership.FieldPreviousOwner])
		})
	}
}

// A watch change on an open (or escalated) aggregate is a "New escalation"
// comment with no state change: ownership is untouched.
func TestAggregate_OwnershipKeptOnWatchChangeWithoutTransition(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name": "AggOwnWO", "condition": []any{"=", "a", "wo"},
		"fields": []string{"a"}, "watch": []string{"c"},
		"throttle": int64(900), "flapping": int64(3),
	})
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "wo", "c": "1"}})
	ownedAs(t, host, first.Hash, "esc", "alice")

	out, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "wo", "c": "2"}})
	require.Equal(t, "esc", out.State)
	requireOwned(t, out, "alice")
}

func TestAggregate_OwnershipClearedOnAutoReopen(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name": "AggOwnR", "condition": []any{"=", "a", "r"},
		"fields": []string{"a"}, "throttle": int64(900), "flapping": int64(3),
	})
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "r"}})
	ownedAs(t, host, first.Hash, "close", "alice")

	out, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "r"}})
	require.Equal(t, "open", out.State)
	requireCleared(t, out, "alice")
}

// The severity-rise throttle bypass re-escalates an ack (clear) but only
// comments on an open aggregate (kept).
func TestAggregate_OwnershipOnSeverityRiseBypass(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		from, to string
		cleared  bool
	}{
		"ack re-escalated": {"ack", "esc", true},
		"open commented":   {"open", "open", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			host := newTestHost(t)
			writeRule(t, host, db.Document{
				"name": "AggOwnS", "condition": []any{"=", "a", "s"},
				"fields": []string{"a"}, "throttle": int64(900),
			})
			p := freshPlugin(t, host)

			first, _ := runProcess(t, p, host, snoozetypes.Record{Severity: "warning", Extra: map[string]any{"a": "s"}})
			ownedAs(t, host, first.Hash, tc.from, "alice")

			out, _ := runProcess(t, p, host, snoozetypes.Record{Severity: "critical", Extra: map[string]any{"a": "s"}})
			require.Equal(t, tc.to, out.State)
			if tc.cleared {
				requireCleared(t, out, "alice")
			} else {
				requireOwned(t, out, "alice")
			}
		})
	}
}

// Outside the throttle window a repeat of an acked aggregate is a "New
// escalation" ack→esc: cleared.
func TestAggregate_OwnershipClearedOnRepeatOutsideThrottle(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name": "AggOwnT", "condition": []any{"=", "a", "t"},
		"fields": []string{"a"}, "throttle": int64(0),
	})
	// The real clock shares the driver's date_epoch stamping, so throttle 0
	// really is "outside the window" (see freshPluginRealClock).
	p := freshPluginRealClock(t, host)

	first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "t"}})
	ownedAs(t, host, first.Hash, "ack", "alice")

	out, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "t"}})
	require.Equal(t, "esc", out.State)
	requireCleared(t, out, "alice")
}

// An automatic close on an OK severity keeps the owner — whoever was handling
// the alert handled it — and a throttled duplicate carries the owner forward
// so the in-flight record matches the stored row.
func TestAggregate_OwnershipKeptOnAutoCloseAndThrottledDuplicate(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name": "AggOwnC", "condition": []any{"=", "a", "c"},
		"fields": []string{"a"}, "throttle": int64(900),
	})
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "c"}})
	ownedAs(t, host, first.Hash, "ack", "alice")

	dup, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "c"}})
	require.Equal(t, "ack", dup.State)
	requireOwned(t, dup, "alice")

	closed, _ := runProcess(t, p, host, snoozetypes.Record{State: "close", Extra: map[string]any{"a": "c"}})
	require.Equal(t, "close", closed.State)
	requireOwned(t, closed, "alice")
}

// Ownership is server-managed: an alert payload that happens to carry an
// `owner` key (an AlertManager label, say) must not reassign an aggregate
// someone is already working on.
func TestAggregate_PayloadCannotOverrideStoredOwner(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name": "AggOwnP", "condition": []any{"=", "a", "p"},
		"fields": []string{"a"}, "throttle": int64(900),
	})
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "p"}})
	ownedAs(t, host, first.Hash, "ack", "alice")

	out, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "p", "owner": "team-x"}})
	requireOwned(t, out, "alice")
}

// Only an operator action makes somebody the owner: a payload `owner` key is
// dropped on a first occurrence and on a duplicate of a row that has never been
// owned (a row predating ownership).
func TestAggregate_PayloadCannotSetOwner(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name": "AggOwnN", "condition": []any{"=", "a", "n"},
		"fields": []string{"a"}, "throttle": int64(900),
	})
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "n", "owner": "team-x"}})
	require.NotContains(t, first.Extra, ownership.FieldOwner, "first occurrence")

	dup, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "n", "owner": "team-x"}})
	require.NotContains(t, dup.Extra, ownership.FieldOwner, "duplicate of a never-owned row")
}
