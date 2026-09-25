package comment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/resolutionhold"
)

// A close arms the resolution hold only when a human posted it; an automatic
// (user-less) close and a zero window arm nothing. The re-fire path is covered
// in aggregaterule; this pins what the close writes.
func TestAfterCreate_CloseArmsResolutionHold(t *testing.T) {
	now := time.Unix(3_000_000, 0).UTC()
	for name, tc := range map[string]struct {
		user  string
		hold  time.Duration
		until int64
	}{
		"human close, configured window": {user: "alice", hold: 90 * time.Minute, until: now.Add(90 * time.Minute).Unix()},
		"automatic close":                {user: "", hold: 90 * time.Minute, until: 0},
		"hold disabled":                  {user: "alice", hold: 0, until: 0},
	} {
		t.Run(name, func(t *testing.T) {
			host := newTestHost(t)
			host.cfg = config.Default()
			host.cfg.Housekeeper.ResolutionHold = schema.Duration(tc.hold)
			p := &Plugin{clock: func() time.Time { return now }}
			require.NoError(t, p.PostInit(guardCtx(), host))

			uid := seedRecord(t, host, "ack")
			require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
				db.Document{resolutionhold.FieldNoted: true}, false))
			doc := map[string]any{"record_uid": uid, "type": "close", "message": "fixed", "user": tc.user}
			require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

			rec := recordDoc(t, host, uid)
			require.Equal(t, "close", rec["state"])
			require.Equal(t, tc.until, asInt64(t, rec[resolutionhold.FieldUntil]))
			require.Equal(t, false, rec[resolutionhold.FieldNoted], "a new close starts a fresh narration")
		})
	}
}

// A manual re-open ends a running hold.
func TestAfterCreate_OpenEndsResolutionHold(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{clock: func() time.Time { return time.Unix(3_000_000, 0) }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "close")
	require.NoError(t, host.DB().UpdateOne(guardCtx(), "record", uid,
		db.Document{resolutionhold.FieldUntil: int64(3_100_000)}, false))
	doc := map[string]any{"record_uid": uid, "type": "open", "message": "not fixed", "user": "bob"}
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{doc}))

	require.Equal(t, int64(0), asInt64(t, recordDoc(t, host, uid)[resolutionhold.FieldUntil]))
}
