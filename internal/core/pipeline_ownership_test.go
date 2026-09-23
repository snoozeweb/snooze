package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestPipeline_OwnershipClearSurvivesMergeWrite pins the reason the ownership
// clear writes explicit empties (internal/ownership, spec D5): the pipeline
// persists an aggregate duplicate with a MERGE write that leaves absent keys
// untouched, and the ownership keys are untyped, so they reach the driver only
// through Record.Extra (aggregaterule's mergeMapIntoRecord → recordToDoc).
//
// Driven through the real aggregaterule + snooze plugins over a real SQLite
// driver so the whole chain is exercised: if any hop dropped the "" / 0
// values, the stored row would keep the old owner and this test would fail.
func TestPipeline_OwnershipClearSurvivesMergeWrite(t *testing.T) {
	t.Parallel()
	c, drv, _, ctx := newThrottleCore(t, 3)

	alert := func(severity string) snoozetypes.Record {
		rec := wafAlert()
		rec.Severity = severity
		return rec
	}
	stored := func(uid string) db.Document {
		doc, err := drv.GetOne(ctx, recordCollection, db.Document{"uid": uid})
		require.NoError(t, err)
		return doc
	}
	own := func(uid, state string) {
		patch := db.Document{"state": state}
		for k, v := range ownership.Take("alice", "ldap", 1234) {
			patch[k] = v
		}
		_, err := drv.SetFields(ctx, recordCollection, patch, condition.Equals("uid", uid))
		require.NoError(t, err)
	}

	first, _, err := c.ProcessRecord(ctx, alert("warning"))
	require.NoError(t, err)
	// A first occurrence gets its uid from the driver on insert.
	row, err := drv.GetOne(ctx, recordCollection, db.Document{"hash": first.Hash})
	require.NoError(t, err)
	uid, _ := row["uid"].(string)
	require.NotEmpty(t, uid)

	// A throttled duplicate of an owned ack keeps the owner on disk and in
	// flight.
	own(uid, "ack")
	dup, _, err := c.ProcessRecord(ctx, alert("warning"))
	require.NoError(t, err)
	require.Equal(t, "alice", dup.Extra[ownership.FieldOwner])
	require.Equal(t, "alice", stored(uid)[ownership.FieldOwner])

	// The rule watches severity: a change on an acked aggregate re-escalates
	// it, which clears the owner.
	esc, _, err := c.ProcessRecord(ctx, alert("critical"))
	require.NoError(t, err)
	require.Equal(t, "esc", esc.State)

	doc := stored(uid)
	require.Equal(t, "esc", doc["state"])
	require.Equal(t, "", doc[ownership.FieldOwner], "the explicit empty must reach the stored row")
	require.Equal(t, "", doc[ownership.FieldOwnerMethod])
	require.EqualValues(t, 0, doc[ownership.FieldOwnerSince])
	require.Equal(t, "alice", doc[ownership.FieldPreviousOwner])
	require.Equal(t, "ldap", doc[ownership.FieldPreviousOwnerMethod])
	require.False(t, condition.Match(doc, ownership.OwnedCond()))

	// Closed and owned, then received again: the auto re-open clears too.
	own(uid, "close")
	reopened, _, err := c.ProcessRecord(ctx, alert("critical"))
	require.NoError(t, err)
	require.Equal(t, "open", reopened.State)
	doc = stored(uid)
	require.Equal(t, "open", doc["state"])
	require.Equal(t, "", doc[ownership.FieldOwner])
	require.Equal(t, "alice", doc[ownership.FieldPreviousOwner])
}
