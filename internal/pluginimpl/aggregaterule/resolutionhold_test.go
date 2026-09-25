package aggregaterule

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/resolutionhold"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// frozenNow is freshPlugin's clock.
const frozenNow = int64(1_700_000_000)

// closedAsFixed stands in for the resolve workflow between two occurrences: an
// operator closes the aggregate (taking ownership, arming a hold ending at
// until) and its analysis carries verdict.
//
// It is re-applied before every occurrence a test wants held, because the test
// harness persists with ReplaceOne and so drops `agentic` (a protected field the
// plugin never carries forward — the production merge write keeps it).
func closedAsFixed(t *testing.T, host *testHost, hash, verdict string, until int64, noted bool) {
	t.Helper()
	patch := db.Document{
		"state":                   "close",
		resolutionhold.FieldUntil: until,
		resolutionhold.FieldNoted: noted,
		"agentic":                 map[string]any{"remediation_plan": map[string]any{"status": verdict}},
	}
	for k, v := range ownership.Take("alice", "ldap", 111) {
		patch[k] = v
	}
	_, err := host.driver.SetFields(tctx(), recordCollection, patch, condition.Equals("hash", hash))
	require.NoError(t, err)
}

func holdRule(t *testing.T, host *testHost) {
	t.Helper()
	writeRule(t, host, db.Document{
		"name": "AggHold", "condition": []any{"=", "a", "h"},
		"fields": []string{"a"}, "watch": []string{"c"},
		"throttle": int64(900), "flapping": int64(3),
	})
}

func firing(c string) snoozetypes.Record {
	return snoozetypes.Record{Severity: "critical", Extra: map[string]any{"a": "h", "c": c}}
}

func TestResolutionHold_KeepsFixedAlertClosed(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	holdRule(t, host)
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, firing("1"))
	until := frozenNow + 3600
	closedAsFixed(t, host, first.Hash, snoozetypes.PlanResolved, until, false)

	out, action := runProcess(t, p, host, firing("1"))
	require.Equal(t, plugins.ActionAbortUpdate, action, "held: persisted, never notified")
	require.Equal(t, "close", out.State)
	requireOwned(t, out, "alice")
	require.Equal(t, true, out.Extra[resolutionhold.FieldNoted])

	stored := recordsByAggregate(t, host, "AggHold")
	require.Len(t, stored, 1)
	require.Equal(t, "close", stored[0]["state"])
	require.Equal(t, int64(2), toInt64(stored[0]["duplicates"], 0), "a held occurrence is still counted")
	require.Equal(t, until, toInt64(stored[0][resolutionhold.FieldUntil], 0))

	comments := commentsByRecord(t, host, aggregateUID(t, host, "AggHold"))
	require.Len(t, comments, 1)
	require.Equal(t, "comment", comments[0]["type"])
	require.Contains(t, comments[0]["message"], "kept closed (hold until 2023-11-14T23:13:20Z)")

	// Second held occurrence — this time a watch-field change, which would
	// otherwise re-open from the watchlist: still closed, no second note.
	closedAsFixed(t, host, first.Hash, snoozetypes.PlanResolved, until, true)
	out, action = runProcess(t, p, host, firing("2"))
	require.Equal(t, plugins.ActionAbortUpdate, action)
	require.Equal(t, "close", out.State)
	requireOwned(t, out, "alice")
	require.Len(t, commentsByRecord(t, host, aggregateUID(t, host, "AggHold")), 1, "the hold is narrated once")
}

func TestResolutionHold_ReopensWhenNotHeld(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		verdict  string
		until    int64
		severity string
	}{
		"hold elapsed":               {snoozetypes.PlanResolved, frozenNow - 1, "critical"},
		"no hold (automatic close)":  {snoozetypes.PlanResolved, 0, "critical"},
		"verdict is not a fix":       {snoozetypes.PlanActionRequired, frozenNow + 3600, "critical"},
		"severity rose past the fix": {snoozetypes.PlanResolved, frozenNow + 3600, "emergency"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			host := newTestHost(t)
			holdRule(t, host)
			p := freshPlugin(t, host)

			first, _ := runProcess(t, p, host, firing("1"))
			closedAsFixed(t, host, first.Hash, tc.verdict, tc.until, false)

			in := firing("1")
			in.Severity = tc.severity
			out, action := runProcess(t, p, host, in)
			require.Equal(t, plugins.ActionContinue, action)
			require.Equal(t, "open", out.State)
			requireCleared(t, out, "alice")
			require.Equal(t, int64(0), toInt64(out.Extra[resolutionhold.FieldUntil], -1), "re-open ends any hold")

			comments := commentsByRecord(t, host, aggregateUID(t, host, "AggHold"))
			require.Len(t, comments, 1)
			require.Equal(t, "Auto re-opened", comments[0]["message"])
		})
	}
}

// The source catching up with the fix ends the hold quietly, so the next
// occurrence — a new incident — re-opens as usual.
func TestResolutionHold_RecoveryEndsHold(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	holdRule(t, host)
	p := freshPlugin(t, host)

	first, _ := runProcess(t, p, host, firing("1"))
	closedAsFixed(t, host, first.Hash, snoozetypes.PlanResolved, frozenNow+3600, false)

	out, action := runProcess(t, p, host, snoozetypes.Record{State: "close", Severity: "ok", Extra: map[string]any{"a": "h", "c": "1"}})
	require.Equal(t, plugins.ActionAbortUpdate, action)
	require.Equal(t, "close", out.State)
	require.Equal(t, int64(0), toInt64(out.Extra[resolutionhold.FieldUntil], -1))
	require.Empty(t, commentsByRecord(t, host, aggregateUID(t, host, "AggHold")), "recovery during a hold is silent")

	// The harness dropped agentic; restore it but keep the released deadline.
	closedAsFixed(t, host, first.Hash, snoozetypes.PlanResolved, 0, false)
	out, _ = runProcess(t, p, host, firing("1"))
	require.Equal(t, "open", out.State)
}

// An alert payload can neither arm a hold on a first occurrence nor rewrite a
// stored one on a duplicate.
func TestResolutionHold_PayloadCannotForgeHold(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	holdRule(t, host)
	p := freshPlugin(t, host)

	forged := firing("1")
	forged.Extra[resolutionhold.FieldUntil] = frozenNow + 99999
	first, _ := runProcess(t, p, host, forged)
	_, present := first.Extra[resolutionhold.FieldUntil]
	require.False(t, present, "first occurrence: payload hold stripped")

	closedAsFixed(t, host, first.Hash, snoozetypes.PlanResolved, frozenNow-1, false)
	forged = firing("1")
	forged.Extra[resolutionhold.FieldUntil] = frozenNow + 99999
	out, _ := runProcess(t, p, host, forged)
	require.Equal(t, "open", out.State, "the stored (elapsed) hold wins over the payload")
}
