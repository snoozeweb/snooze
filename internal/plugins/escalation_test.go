package plugins

import (
	"encoding/json"
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
		Severity:         "critical",
		Extra: map[string]any{
			"duplicates":        int64(12),
			"previous_severity": "warning",
			"trend_indication":  "moreSevere",
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

// TestSeverityRoseUsesTheRealSeverities is the regression test for a shipped
// bug: SeverityRose originally read `trend_indication` and compared it to "up".
// The field's real vocabulary is Alerta's ("moreSevere" / "lessSevere" /
// "noChange"), so the check was ALWAYS FALSE in production and the priority bump
// in jira, servicenow and opsgenie silently never happened. The original tests
// passed only because their fixtures invented the value "up".
//
// The severities are now compared directly, so no label vocabulary can break
// it. These cases deliberately stamp the REAL trend labels to prove the answer
// no longer depends on them.
func TestSeverityRoseUsesTheRealSeverities(t *testing.T) {
	cases := map[string]struct {
		severity, previous, trend string
		want                      bool
	}{
		"rose_warning_to_critical": {"critical", "warning", "moreSevere", true},
		"rose_info_to_error":       {"error", "info", "moreSevere", true},
		"fell_critical_to_warning": {"warning", "critical", "lessSevere", false},
		"unchanged":                {"warning", "warning", "noChange", false},
		// A rise must be reported even when the trend label is absent entirely,
		// which is the case for a record that never passed through
		// aggregaterule.
		"rose_with_no_trend_label": {"critical", "warning", "", true},
		// And a stale/wrong label must not be able to fake a rise.
		"lying_label":          {"warning", "critical", "moreSevere", false},
		"no_previous_severity": {"critical", "", "", false},
		"no_current_severity":  {"", "warning", "moreSevere", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := EscalationFrom(snoozetypes.Record{
				EscalationCount: 1,
				Severity:        tc.severity,
				Extra: map[string]any{
					"previous_severity": tc.previous,
					"trend_indication":  tc.trend,
				},
			})
			require.Equal(t, tc.want, e.SeverityRose())
		})
	}
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

// TestMarshalRecordIncludesExtra is the fix for a real data-loss bug: a
// notifier forwarding the "whole record" used to drop everything the pipeline
// stamped without a typed home — including the notify_ref handles a companion
// daemon needs to avoid duplicating a ticket.
func TestMarshalRecordIncludesExtra(t *testing.T) {
	raw, err := MarshalRecord(snoozetypes.Record{
		Host:            "db-1",
		EscalationCount: 2,
		Extra: map[string]any{
			"duplicates":               float64(7),
			"notify_ref_Create ticket": map[string]any{"issue_key": "OPS-1"},
		},
	})
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, "db-1", doc["host"])
	require.EqualValues(t, 2, doc["escalation_count"])
	require.EqualValues(t, 7, doc["duplicates"], "aggregaterule counters must survive")
	ref, _ := doc["notify_ref_Create ticket"].(map[string]any)
	require.Equal(t, "OPS-1", ref["issue_key"], "the notifier handle must survive")
}

// Typed fields win, so the change is purely additive: no existing key can
// change shape or value because of an Extra entry.
func TestMarshalRecordTypedFieldsWin(t *testing.T) {
	raw, err := MarshalRecord(snoozetypes.Record{
		Host:  "typed",
		Extra: map[string]any{"host": "extra"},
	})
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, "typed", doc["host"])
}

// With no Extra the output is exactly what json.Marshal produced before, so a
// consumer of the default webhook body sees no change at all. (A zero
// time.Time is not elided by omitempty — pre-existing behaviour, asserted here
// so a future change to it is a deliberate one.)
func TestMarshalRecordWithoutExtra(t *testing.T) {
	rec := snoozetypes.Record{Host: "db-1"}
	raw, err := MarshalRecord(rec)
	require.NoError(t, err)
	plain, err := json.Marshal(rec)
	require.NoError(t, err)
	require.JSONEq(t, string(plain), string(raw))
}

func TestBanner(t *testing.T) {
	require.Empty(t, Escalation{}.Banner())
	require.Equal(t, "New escalation #1", Escalation{Count: 1}.Banner())
	require.Equal(t, "New escalation #2 (timeout)",
		Escalation{Count: 2, Reason: "timeout"}.Banner())
}

func TestPrefixMessage(t *testing.T) {
	// A first fire is returned untouched, so a notifier can prefix
	// unconditionally.
	require.Equal(t, "disk full", Escalation{}.PrefixMessage("disk full"))
	require.Equal(t, "⚠️ New escalation #1\ndisk full",
		Escalation{Count: 1}.PrefixMessage("disk full"))
	require.Equal(t, "⚠️ New escalation #1", Escalation{Count: 1}.PrefixMessage(""))
}

// A batch-aware receiver answers `{"<hash>": {...}}` even for a single alert,
// and servers before webhook's unwrapForRecord stamped that envelope verbatim.
// NotifyRef must read through it, otherwise every notifier holding such a
// record takes its create path again and duplicates the ticket / message.
func TestNotifyRefReadsThroughBatchEnvelope(t *testing.T) {
	rec := snoozetypes.Record{
		Hash: "ec2f9135",
		Extra: map[string]any{
			"response_Jira Ticket": map[string]any{
				"ec2f9135": map[string]any{"issue_key": "CG-1811"},
			},
		},
	}
	require.Equal(t, "CG-1811", NotifyRefString(rec, "Jira Ticket", "issue_key"))
}

// Another alert's entry is not this alert's handle.
func TestNotifyRefIgnoresForeignBatchEntry(t *testing.T) {
	rec := snoozetypes.Record{
		Hash: "mine",
		Extra: map[string]any{
			"response_act": map[string]any{"theirs": map[string]any{"issue_key": "CG-1"}},
		},
	}
	require.Empty(t, NotifyRefString(rec, "act", "issue_key"))
}

// A handle that happens to carry a key equal to the record hash is still read
// as a flat handle: unwrapping only kicks in when the nested value is an
// object, and the flat lookup is tried first anyway.
func TestNotifyRefFlatHandleUnaffectedByHash(t *testing.T) {
	rec := snoozetypes.Record{
		Hash:  "h1",
		Extra: map[string]any{"notify_ref_act": map[string]any{"issue_key": "CG-2", "h1": "scalar"}},
	}
	require.Equal(t, "CG-2", NotifyRefString(rec, "act", "issue_key"))
}
