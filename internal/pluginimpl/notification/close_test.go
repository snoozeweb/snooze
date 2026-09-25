package notification

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// closeRecorder is a CloseNotifier that records every NotifyClose.
type closeRecorder struct {
	recordingNotifier
	mu     sync.Mutex
	events []plugins.CloseEvent
	metas  []map[string]any
	err    error
}

func (c *closeRecorder) NotifyClose(_ context.Context, _ snoozetypes.Record, payload plugins.NotificationPayload, ev plugins.CloseEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	c.metas = append(c.metas, payload.Meta)
	return c.err
}

func seedClosedRecord(t *testing.T, h *testHost, extra db.Document) string {
	t.Helper()
	doc := db.Document{"host": "db-01", "hash": "h-close", "state": "close"}
	for k, v := range extra {
		doc[k] = v
	}
	res, err := h.driver.Write(tctx(), recordCollectionName, []db.Document{doc}, db.WriteOptions{})
	require.NoError(t, err)
	return res.Added[0]
}

func TestDispatchClose_ReachesHandleHolders(t *testing.T) {
	host := newHost(t)
	jira := &closeRecorder{recordingNotifier: recordingNotifier{name: "jira"}}
	plain := &recordingNotifier{name: "mail"}
	host.plugins["jira"] = jira
	host.plugins["mail"] = plain
	writeActions(t, host, []map[string]any{
		{"name": "Jira Escalation", "action": map[string]any{"selected": "jira", "subcontent": map[string]any{"project_key": "AD"}}},
		{"name": "Mail ops", "action": map[string]any{"selected": "mail", "subcontent": map[string]any{}}},
	})
	uid := seedClosedRecord(t, host, db.Document{
		"notify_ref_Jira Escalation": map[string]any{"issue_key": "AD-795"},
		"notify_ref_Mail ops":        map[string]any{"message_id": "x"},
		"notification_from":          map[string]any{"name": "Kube Prod"},
	})
	_, err := host.driver.Write(tctx(), "comment", []db.Document{
		{"record_uid": uid, "message": "Resolved: old note", "date_epoch": int64(100)},
		{"record_uid": uid, "message": "Resolved: re-ran the backup", "date_epoch": int64(200)},
		{"record_uid": uid, "message": "just a comment", "date_epoch": int64(300)},
	}, db.WriteOptions{})
	require.NoError(t, err)

	p := newPlugin(t, host)
	p.dispatchClose(tctx(), uid, plugins.CloseEvent{Actor: "alice", At: time.Unix(500, 0)})

	require.Len(t, jira.events, 1, "only a CloseNotifier with a handle hears the close")
	require.Equal(t, "re-ran the backup", jira.events[0].Resolution, "the newest Resolved: note")
	require.Equal(t, "Jira Escalation", jira.metas[0]["action_name"])
	require.Equal(t, "AD", jira.metas[0]["project_key"])
	require.Equal(t, "Kube Prod", jira.metas[0]["notification_name"])
	require.Empty(t, plain.Calls(), "a plain notifier is never re-sent on close")
}

// A failed sync lands on the alert's timeline; the close stands.
func TestDispatchClose_FailureOnTimeline(t *testing.T) {
	host := newHost(t)
	jira := &closeRecorder{recordingNotifier: recordingNotifier{name: "jira"}, err: errors.New("HTTP 403")}
	host.plugins["jira"] = jira
	writeActions(t, host, []map[string]any{
		{"name": "Jira Escalation", "action": map[string]any{"selected": "jira", "subcontent": map[string]any{}}},
	})
	uid := seedClosedRecord(t, host, db.Document{
		"notify_ref_Jira Escalation": map[string]any{"issue_key": "AD-795"},
		"comment_count":              int64(3),
	})

	newPlugin(t, host).dispatchClose(tctx(), uid, plugins.CloseEvent{At: time.Unix(500, 0)})

	docs, _, err := host.driver.Search(tctx(), "comment", condition.Equals("record_uid", uid), db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Contains(t, docs[0]["message"], `Close not synced to action "Jira Escalation": HTTP 403`)
	rec, err := host.driver.GetOne(tctx(), recordCollectionName, db.Document{"uid": uid})
	require.NoError(t, err)
	require.Equal(t, "close", rec["state"])
	require.EqualValues(t, 4, docEpoch(rec["comment_count"]))
}

func TestDispatchClose_NoHandleIsNoop(t *testing.T) {
	host := newHost(t)
	jira := &closeRecorder{recordingNotifier: recordingNotifier{name: "jira"}}
	host.plugins["jira"] = jira
	uid := seedClosedRecord(t, host, nil)
	newPlugin(t, host).dispatchClose(tctx(), uid, plugins.CloseEvent{At: time.Unix(500, 0)})
	require.Empty(t, jira.events)
}
