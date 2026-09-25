package jira

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

var closedAt = time.Unix(1_790_339_000, 0).UTC()

func closeSetup(t *testing.T, extra map[string]any) (*jiraRecorder, *Plugin, map[string]any, map[string]any) {
	t.Helper()
	jira, srv := newJIRARecorder(t)
	jira.transitions = []map[string]any{
		{"id": "41", "name": "Close issue", "to": map[string]any{"name": "Done"}},
	}
	meta := testMeta()
	meta["jira_url"] = srv.URL
	for k, v := range extra {
		meta[k] = v
	}
	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
	return jira, testPlugin(srv), meta, stamped
}

// A human close comments who closed it and what fixed it; no transition unless
// one is configured.
func TestNotifyClose_CommentsOnHumanClose(t *testing.T) {
	jira, p, meta, stamped := closeSetup(t, nil)
	ev := plugins.CloseEvent{Actor: "alice", At: closedAt, Resolution: "re-ran the backup job"}

	require.NoError(t, p.NotifyClose(context.Background(), recWithRef(stamped), escPayload(meta, 0, stamped), ev))

	comments := jira.matching(http.MethodPost, "/comment")
	require.Len(t, comments, 1)
	text := adfPlainText(t, comments[0].Body["body"])
	require.Contains(t, text, "closed by alice at 2026-09-25T")
	require.Contains(t, text, "Resolution: re-ran the backup job")
	require.Empty(t, jira.matching(http.MethodPost, "/transitions"), "no close_transition configured")
	require.Equal(t, closedAt.Unix(),
		refInt64(plugins.NotifyRef(snoozetypes.Record{Extra: stamped}, "Create ticket")[refCloseSynced]))
	require.Equal(t, "OPS-1",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Create ticket", refIssueKey),
		"the issue key survives the merge")
}

// The same close dispatched twice comments once.
func TestNotifyClose_Idempotent(t *testing.T) {
	jira, p, meta, stamped := closeSetup(t, nil)
	ev := plugins.CloseEvent{Reason: "Severity critical => ok", At: closedAt}
	for range 2 {
		require.NoError(t, p.NotifyClose(context.Background(), recWithRef(stamped), escPayload(meta, 0, stamped), ev))
	}
	comments := jira.matching(http.MethodPost, "/comment")
	require.Len(t, comments, 1)
	require.Contains(t, adfPlainText(t, comments[0].Body["body"]), "closed automatically at")
}

func TestNotifyClose_Transition(t *testing.T) {
	for name, tc := range map[string]struct {
		transition string
		category   string
		wantID     string
	}{
		"by status name":         {transition: "Done", category: "indeterminate", wantID: "41"},
		"by numeric id":          {transition: "77", category: "indeterminate", wantID: "77"},
		"already resolved: skip": {transition: "Done", category: "done"},
	} {
		t.Run(name, func(t *testing.T) {
			jira, p, meta, stamped := closeSetup(t, map[string]any{"close_transition": tc.transition})
			jira.issueStatusCategory = tc.category
			require.NoError(t, p.NotifyClose(context.Background(), recWithRef(stamped),
				escPayload(meta, 0, stamped), plugins.CloseEvent{Actor: "alice", At: closedAt}))
			posts := jira.matching(http.MethodPost, "/transitions")
			if tc.wantID == "" {
				require.Empty(t, posts)
				return
			}
			require.Len(t, posts, 1)
			require.Equal(t, tc.wantID, posts[0].Body["transition"].(map[string]any)["id"])
		})
	}
}

// A transition the workflow does not offer is an error (the dispatcher puts it
// on the alert timeline), but the comment has landed and is recorded.
func TestNotifyClose_UnknownTransitionReported(t *testing.T) {
	jira, p, meta, stamped := closeSetup(t, map[string]any{"close_transition": "Closed"})
	err := p.NotifyClose(context.Background(), recWithRef(stamped), escPayload(meta, 0, stamped),
		plugins.CloseEvent{Actor: "alice", At: closedAt})
	require.ErrorContains(t, err, `offers no transition to "Closed"`)
	require.Len(t, jira.matching(http.MethodPost, "/comment"), 1)
	require.NotNil(t, plugins.NotifyRef(snoozetypes.Record{Extra: stamped}, "Create ticket")[refCloseSynced])
}

func TestNotifyClose_NoOps(t *testing.T) {
	for name, tc := range map[string]struct {
		meta  map[string]any
		ev    plugins.CloseEvent
		noRef bool
	}{
		"on_close skip":       {meta: map[string]any{"on_close": "skip"}, ev: plugins.CloseEvent{Actor: "alice", At: closedAt}},
		"closed from JIRA":    {ev: plugins.CloseEvent{Actor: "svc", Channel: "jira", At: closedAt}},
		"no ticket on record": {ev: plugins.CloseEvent{Actor: "alice", At: closedAt}, noRef: true},
	} {
		t.Run(name, func(t *testing.T) {
			jira, p, meta, stamped := closeSetup(t, tc.meta)
			if tc.noRef {
				stamped = map[string]any{}
			}
			require.NoError(t, p.NotifyClose(context.Background(), recWithRef(stamped), escPayload(meta, 0, stamped), tc.ev))
			require.Empty(t, jira.calls())
		})
	}
}

func TestConfig_OnCloseValidated(t *testing.T) {
	meta := testMeta()
	meta["jira_url"] = "https://example.atlassian.net"
	meta["on_close"] = "close-it"
	_, err := configFromMeta(meta)
	require.ErrorContains(t, err, "on_close")
}
