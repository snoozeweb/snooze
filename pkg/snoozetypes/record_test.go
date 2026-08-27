package snoozetypes

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRecord_HashRoundTrips guards the snooze-server → snooze-teams wire hop.
// Hash MUST be a typed field, not buried in Extra, because Extra carries
// `json:"-"` and is silently dropped by encoding/json — that is the regression
// that left every Teams alert's host name unlinked: the daemon received the
// alert JSON, the hash field had no typed home, recordWebURL returned "" and
// hostText fell back to bare text with no Markdown link.
func TestRecord_HashRoundTrips(t *testing.T) {
	in := Record{
		Host: "db-1.example.com",
		Hash: "0123456789abcdef0123456789abcdef",
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"hash":"0123456789abcdef0123456789abcdef"`,
		"Hash must serialize so snooze-teams receives it on the wire")

	var out Record
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, in.Hash, out.Hash,
		"Hash must survive an Unmarshal so the daemon can build the host link URL")
}

// TestRecordFromDocument covers the projection notifiers depend on. The bug it
// guards: a bare json.Unmarshal into a Record drops Extra (`json:"-"`), taking
// the notify_ref_<action> handles with it, so a notifier finds no handle and
// duplicates the ticket it already created.
func TestRecordFromDocument(t *testing.T) {
	rec, err := RecordFromDocument(map[string]any{
		"uid":                      "r1",
		"host":                     "db-1",
		"severity":                 "critical",
		"state":                    "esc",
		"escalation_count":         2,
		"escalation_reason":        "timeout",
		"notify_ref_Create ticket": map[string]any{"issue_key": "OPS-1"},
		"duplicates":               int64(4),
		"previous_severity":        "warning",
	})
	require.NoError(t, err)

	// Typed fields.
	require.Equal(t, "r1", rec.UID)
	require.Equal(t, "esc", rec.State)
	require.Equal(t, 2, rec.EscalationCount)
	require.Equal(t, "timeout", rec.EscalationReason)

	// Untyped fields must survive — this is the whole point.
	require.Equal(t, int64(4), rec.Extra["duplicates"])
	require.Equal(t, "warning", rec.Extra["previous_severity"])
	require.Contains(t, rec.Extra, "notify_ref_Create ticket")

	// Typed fields must NOT be duplicated into Extra.
	for _, k := range []string{"uid", "host", "state", "escalation_count"} {
		require.NotContains(t, rec.Extra, k)
	}
}

// Driver bookkeeping must not ride along into a notifier's outbound payload.
func TestRecordFromDocumentDropsPrivateKeys(t *testing.T) {
	rec, err := RecordFromDocument(map[string]any{
		"uid":  "r1",
		"_id":  "mongo-oid",
		"_old": map[string]any{"state": "open"},
		"keep": "yes",
	})
	require.NoError(t, err)
	require.NotContains(t, rec.Extra, "_id")
	require.NotContains(t, rec.Extra, "_old")
	require.Equal(t, "yes", rec.Extra["keep"])
}

// A document with nothing untyped leaves Extra nil rather than an empty map, so
// downstream nil checks behave.
func TestRecordFromDocumentNoExtra(t *testing.T) {
	rec, err := RecordFromDocument(map[string]any{"uid": "r1", "host": "h"})
	require.NoError(t, err)
	require.Nil(t, rec.Extra)
}
