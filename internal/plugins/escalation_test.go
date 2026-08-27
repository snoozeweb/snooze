package plugins

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestEscalationFromZeroValue is the load-bearing guarantee of the whole
// feature: a record that predates the escalation fields must project to a
// zero Escalation, which every notifier reads as "first fire".
func TestEscalationFromZeroValue(t *testing.T) {
	e := EscalationFrom(snoozetypes.Record{Host: "h1"})
	require.Equal(t, 0, e.Count)
	require.False(t, e.IsRe())
	require.False(t, e.SeverityRose())
	require.Empty(t, e.Ordinal())
	require.True(t, e.At.IsZero())
}

func TestEscalationFromTypedFields(t *testing.T) {
	e := EscalationFrom(snoozetypes.Record{
		EscalationCount:  3,
		EscalationReason: "manual",
		EscalationActor:  "alice",
		EscalatedAt:      1700000000,
		Extra: map[string]any{
			"duplicates":        int64(12),
			"previous_severity": "warning",
			"trend_indication":  "up",
		},
	})
	require.True(t, e.IsRe())
	require.Equal(t, 3, e.Count)
	require.Equal(t, "#3", e.Ordinal())
	require.Equal(t, "manual", e.Reason)
	require.Equal(t, "alice", e.Actor)
	require.Equal(t, time.Unix(1700000000, 0).UTC(), e.At)
	require.Equal(t, int64(12), e.Duplicates)
	require.Equal(t, "warning", e.PreviousSeverity)
	require.True(t, e.SeverityRose())
}

// A record that came back out of the driver carries the escalation fields in
// Extra (untyped, and numeric in whichever shape the driver decoded), not in
// the typed fields. Both paths must project identically.
func TestEscalationFromExtraFallback(t *testing.T) {
	for name, raw := range map[string]any{
		"int64":   int64(2),
		"int":     2,
		"float64": float64(2),
	} {
		t.Run(name, func(t *testing.T) {
			e := EscalationFrom(snoozetypes.Record{Extra: map[string]any{
				"escalation_count":  raw,
				"escalation_reason": "timeout",
				"escalation_actor":  "bob",
				"escalated_at":      int64(1700000001),
			}})
			require.Equal(t, 2, e.Count)
			require.Equal(t, "timeout", e.Reason)
			require.Equal(t, "bob", e.Actor)
			require.Equal(t, time.Unix(1700000001, 0).UTC(), e.At)
		})
	}
}

func TestEscalationTrendDown(t *testing.T) {
	e := EscalationFrom(snoozetypes.Record{
		EscalationCount: 1,
		Extra:           map[string]any{"trend_indication": "down"},
	})
	require.False(t, e.SeverityRose(), "a severity drop must not raise a ticket priority")
}

func TestOrdinal(t *testing.T) {
	require.Empty(t, Escalation{}.Ordinal())
	require.Equal(t, "#1", Escalation{Count: 1}.Ordinal())
	require.Equal(t, "#42", Escalation{Count: 42}.Ordinal())
	require.Equal(t, "#100", Escalation{Count: 100}.Ordinal())
}

// fakeInject records what a notifier stamped onto the record.
func fakeInject(dst map[string]any) InjectFunc {
	return func(field string, value any) { dst[field] = value }
}

func TestStoreAndReadNotifyRef(t *testing.T) {
	stamped := map[string]any{}
	payload := NotificationPayload{
		Meta:   map[string]any{"action_name": "Create ticket"},
		Inject: fakeInject(stamped),
	}
	require.Equal(t, "Create ticket", payload.ActionName())

	StoreNotifyRef(payload, payload.ActionName(), map[string]any{"issue_key": "OPS-7"})
	require.Contains(t, stamped, "notify_ref_Create ticket")

	// Round-trip: the stamped field is what aggregaterule ferries onto the
	// next occurrence's record.
	rec := snoozetypes.Record{Extra: stamped}
	require.Equal(t, "OPS-7", NotifyRefString(rec, "Create ticket", "issue_key"))
	require.Empty(t, NotifyRefString(rec, "Other action", "issue_key"))
	require.Empty(t, NotifyRefString(rec, "Create ticket", "absent"))
}

// Back-compat: the Teams bridge already has `response_<action>.message_ids` on
// live records. Those must keep resolving after this change, with no migration.
func TestNotifyRefReadsLegacyResponseField(t *testing.T) {
	rec := snoozetypes.Record{Extra: map[string]any{
		"response_teams": map[string]any{
			"message_ids": map[string]any{"teams/x/channels/y": "1700000000001"},
		},
	}}
	ref := NotifyRef(rec, "teams")
	require.NotNil(t, ref)
	require.Contains(t, ref, "message_ids")
}

// The canonical field wins when both are present, so a notifier that has
// started writing notify_ref_ isn't dragged back to a stale legacy handle.
func TestNotifyRefPrefersCanonicalOverLegacy(t *testing.T) {
	rec := snoozetypes.Record{Extra: map[string]any{
		"notify_ref_a": map[string]any{"issue_key": "NEW-1"},
		"response_a":   map[string]any{"issue_key": "OLD-1"},
	}}
	require.Equal(t, "NEW-1", NotifyRefString(rec, "a", "issue_key"))
}

func TestNotifyRefTolerantOfGarbage(t *testing.T) {
	require.Nil(t, NotifyRef(snoozetypes.Record{}, "a"))
	require.Nil(t, NotifyRef(snoozetypes.Record{Extra: map[string]any{}}, ""))
	require.Nil(t, NotifyRef(snoozetypes.Record{Extra: map[string]any{"notify_ref_a": "not-a-map"}}, "a"))
	require.Nil(t, NotifyRef(snoozetypes.Record{Extra: map[string]any{"notify_ref_a": map[string]any{}}}, "a"),
		"an empty handle is no handle: the notifier must take the create path")
}

func TestStoreNotifyRefNoOps(t *testing.T) {
	stamped := map[string]any{}
	payload := NotificationPayload{Inject: fakeInject(stamped)}

	StoreNotifyRef(payload, "", map[string]any{"k": "v"})
	StoreNotifyRef(payload, "a", nil)
	StoreNotifyRef(payload, "a", map[string]any{})
	require.Empty(t, stamped)

	// A nil Inject (direct callers, tests) must not panic.
	StoreNotifyRef(NotificationPayload{}, "a", map[string]any{"k": "v"})
}

// A runaway notifier must not be able to bloat every record it touches.
func TestStoreNotifyRefRejectsOversizedHandle(t *testing.T) {
	stamped := map[string]any{}
	payload := NotificationPayload{Inject: fakeInject(stamped)}
	big := make([]byte, maxNotifyRefBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	StoreNotifyRef(payload, "a", map[string]any{"blob": string(big)})
	require.Empty(t, stamped)
}

// An unmarshalable handle is dropped rather than panicking the notifier.
func TestStoreNotifyRefRejectsUnmarshalable(t *testing.T) {
	stamped := map[string]any{}
	payload := NotificationPayload{Inject: fakeInject(stamped)}
	StoreNotifyRef(payload, "a", map[string]any{"fn": func() {}})
	require.Empty(t, stamped)
}

func TestMergeNotifyRefPreservesExistingKeys(t *testing.T) {
	rec := snoozetypes.Record{Extra: map[string]any{
		"notify_ref_a": map[string]any{"issue_key": "OPS-7", "message_id": "m1"},
	}}
	merged := MergeNotifyRef(rec, "a", map[string]any{"message_id": "m2"})
	require.Equal(t, "OPS-7", merged["issue_key"], "an unrelated key must survive")
	require.Equal(t, "m2", merged["message_id"], "the new value must win")

	// The returned map is a copy: mutating it must not touch the record.
	merged["issue_key"] = "MUTATED"
	require.Equal(t, "OPS-7", NotifyRefString(rec, "a", "issue_key"))
}

func TestIsNotifyRefField(t *testing.T) {
	require.True(t, IsNotifyRefField("notify_ref_x"))
	require.True(t, IsNotifyRefField("response_x"))
	require.False(t, IsNotifyRefField("host"))
	require.False(t, IsNotifyRefField("escalation_count"))
}
