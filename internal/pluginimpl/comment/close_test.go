package comment

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// closeSpy stands in for the notification plugin's close fan-out.
type closeSpy struct {
	uids   []string
	events []plugins.CloseEvent
}

func (s *closeSpy) Name() string                                 { return "notification" }
func (s *closeSpy) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: "notification"} }
func (s *closeSpy) PostInit(context.Context, plugins.Host) error { return nil }
func (s *closeSpy) Reload(context.Context) error                 { return nil }
func (s *closeSpy) DispatchClose(_ context.Context, uid string, ev plugins.CloseEvent) {
	s.uids = append(s.uids, uid)
	s.events = append(s.events, ev)
}

// A close comment tells the notifiers; other transitions do not.
func TestAfterCreate_CloseDispatchesToNotifiers(t *testing.T) {
	host := newTestHost(t)
	spy := &closeSpy{}
	host.notif = spy
	now := time.Unix(4_000_000, 0).UTC()
	p := &Plugin{clock: func() time.Time { return now }}
	require.NoError(t, p.PostInit(guardCtx(), host))

	uid := seedRecord(t, host, "ack")
	require.NoError(t, p.AfterCreate(guardCtx(), []map[string]any{
		{"record_uid": uid, "type": "comment", "message": "Resolved: fixed it", "user": "alice"},
		{"record_uid": uid, "type": "close", "message": "done", "user": "alice", "method": "local"},
	}))

	require.Equal(t, []string{uid}, spy.uids)
	require.Equal(t, plugins.CloseEvent{Actor: "alice", Channel: "local", At: now}, spy.events[0])
}
