package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// recordedReq is one request the fake JIRA site received.
type recordedReq struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

// jiraRecorder is a JIRA stand-in that records every request and answers with
// scripted responses. Tests assert on the recorded calls — in particular on how
// many issue CREATES happened, which is the whole point of the feature.
type jiraRecorder struct {
	mu   sync.Mutex
	reqs []recordedReq

	// issueStatusCategory is what GET /issue/{key} reports ("done" or "indeterminate").
	issueStatusCategory string
	// transitions is what GET /issue/{key}/transitions offers.
	transitions []map[string]any
	// commentStatus overrides the POST /comment response status (0 = 201).
	commentStatus int
}

func newJIRARecorder(t *testing.T) (*jiraRecorder, *httptest.Server) {
	t.Helper()
	rec := &jiraRecorder{
		issueStatusCategory: "indeterminate",
		transitions: []map[string]any{
			{"id": "31", "name": "Reopen", "to": map[string]any{"name": "To Do"}},
		},
	}
	srv := newFakeJira(t, nil, rec.handle)
	return rec, srv
}

func (j *jiraRecorder) handle(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	j.mu.Lock()
	j.reqs = append(j.reqs, recordedReq{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: body,
	})
	j.mu.Unlock()

	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/rest/api/3/issue":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"OPS-1","id":"10100"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/comment"):
		status := j.commentStatus
		if status == 0 {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/transitions"):
		_ = json.NewEncoder(w).Encode(map[string]any{"transitions": j.transitions})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/rest/api/3/issue/"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"fields": map[string]any{
				"status": map[string]any{
					"statusCategory": map[string]any{"key": j.issueStatusCategory},
				},
			},
		})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (j *jiraRecorder) calls() []recordedReq {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]recordedReq, len(j.reqs))
	copy(out, j.reqs)
	return out
}

// creates counts POSTs to the issue-create endpoint.
func (j *jiraRecorder) creates() int {
	n := 0
	for _, r := range j.calls() {
		if r.Method == http.MethodPost && r.Path == "/rest/api/3/issue" {
			n++
		}
	}
	return n
}

func (j *jiraRecorder) matching(method, suffix string) []recordedReq {
	var out []recordedReq
	for _, r := range j.calls() {
		if r.Method == method && strings.HasSuffix(r.Path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

// escPayload builds a payload for the given escalation count, capturing every
// notify-ref the notifier stores into stamped.
func escPayload(meta map[string]any, count int, stamped map[string]any) plugins.NotificationPayload {
	meta["action_name"] = "Create ticket"
	p := plugins.NotificationPayload{
		Meta:       meta,
		Escalation: plugins.Escalation{Count: count},
		Inject:     func(field string, value any) { stamped[field] = value },
	}
	return p
}

// recWithRef returns a record carrying the handle a previous delivery stored.
func recWithRef(stamped map[string]any) snoozetypes.Record {
	return snoozetypes.Record{
		Host: "db-01", Severity: "critical", Message: "down",
		Extra: stamped,
	}
}

func testPlugin(srv *httptest.Server) *Plugin {
	return &Plugin{newClient: func(time.Duration) *http.Client { return srv.Client() }}
}

// TestSendStoresIssueKeyOnCreate: the first delivery must leave a handle behind,
// or nothing downstream can avoid duplicating.
func TestSendStoresIssueKeyOnCreate(t *testing.T) {
	_, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	stamped := map[string]any{}
	rec := snoozetypes.Record{Host: "db-01", Severity: "critical", Message: "down"}
	require.NoError(t, p.Send(context.Background(), rec, escPayload(meta, 0, stamped)))

	require.Equal(t, "OPS-1",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Create ticket", refIssueKey))
}

// TestReEscalationNeverCreatesASecondIssue is the load-bearing test for the
// whole change: the behaviour an operator complained about was a new ticket per
// escalation.
func TestReEscalationNeverCreatesASecondIssue(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	stamped := map[string]any{}
	// First fire → one create.
	require.NoError(t, p.Send(context.Background(),
		snoozetypes.Record{Host: "db-01", Severity: "critical", Message: "down"},
		escPayload(meta, 0, stamped)))
	require.Equal(t, 1, jira.creates())

	// Three re-escalations → still one create, three comments.
	for i := 1; i <= 3; i++ {
		require.NoError(t, p.Send(context.Background(), recWithRef(stamped),
			escPayload(meta, i, stamped)))
	}
	require.Equal(t, 1, jira.creates(), "a re-escalation must never open a second ticket")
	require.Len(t, jira.matching(http.MethodPost, "/comment"), 3)
}

// The comment must say which escalation it is and why, so an operator reading
// the ticket can tell a flapping alert from a genuinely new problem.
func TestEscalationCommentCarriesOrdinalAndReason(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL
	meta["notification_name"] = "Page the DBAs"

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
	payload := escPayload(meta, 2, stamped)
	payload.Escalation = plugins.Escalation{Count: 2, Reason: "manual", Actor: "alice"}

	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), payload))

	comments := jira.matching(http.MethodPost, "/comment")
	require.Len(t, comments, 1)
	text := adfPlainText(t, comments[0].Body["body"])
	require.Contains(t, text, "Re-escalation #2")
	require.Contains(t, text, "Reason: manual")
	require.Contains(t, text, "Escalated by: alice")
	require.Contains(t, text, "From Page the DBAs")
	require.Contains(t, text, "Host: db-01")
}

// An operator-supplied template is appended, and a broken template must not
// cost us the comment.
func TestEscalationCommentTemplate(t *testing.T) {
	t.Run("rendered", func(t *testing.T) {
		jira, srv := newJIRARecorder(t)
		p := testPlugin(srv)
		meta := testMeta()
		meta["jira_url"] = srv.URL
		meta["escalation_comment"] = "runbook for {{ .Host }}"

		stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
		require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))

		text := adfPlainText(t, jira.matching(http.MethodPost, "/comment")[0].Body["body"])
		require.Contains(t, text, "runbook for db-01")
	})

	t.Run("broken_template_still_comments", func(t *testing.T) {
		jira, srv := newJIRARecorder(t)
		p := testPlugin(srv)
		meta := testMeta()
		meta["jira_url"] = srv.URL
		meta["escalation_comment"] = "{{ .Nope "

		stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
		require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))
		require.Len(t, jira.matching(http.MethodPost, "/comment"), 1)
	})
}

// Priority is raised only when severity actually rose. Re-escalating at the
// same severity must not silently promote a ticket an operator downgraded.
func TestEscalationRaisesPriorityOnlyWhenSeverityRose(t *testing.T) {
	for name, tc := range map[string]struct {
		trend      string
		wantPutSet bool
	}{
		"severity_rose": {trend: "up", wantPutSet: true},
		"severity_same": {trend: "same", wantPutSet: false},
		"severity_fell": {trend: "down", wantPutSet: false},
		"trend_unknown": {trend: "", wantPutSet: false},
	} {
		t.Run(name, func(t *testing.T) {
			jira, srv := newJIRARecorder(t)
			p := testPlugin(srv)
			meta := testMeta()
			meta["jira_url"] = srv.URL

			stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
			payload := escPayload(meta, 1, stamped)
			payload.Escalation = plugins.Escalation{Count: 1, Trend: tc.trend}

			require.NoError(t, p.Send(context.Background(), recWithRef(stamped), payload))

			puts := jira.matching(http.MethodPut, "/rest/api/3/issue/OPS-1")
			if tc.wantPutSet {
				require.Len(t, puts, 1)
				fields, _ := puts[0].Body["fields"].(map[string]any)
				require.Contains(t, fields, "priority")
			} else {
				require.Empty(t, puts)
			}
		})
	}
}

// A resolved ticket is transitioned back — an alert that is escalating again is
// by definition not resolved. A ticket someone is already working is left alone.
func TestEscalationReopensDoneIssueOnly(t *testing.T) {
	t.Run("done_is_reopened", func(t *testing.T) {
		jira, srv := newJIRARecorder(t)
		jira.issueStatusCategory = "done"
		p := testPlugin(srv)
		meta := testMeta()
		meta["jira_url"] = srv.URL

		stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
		require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))

		trs := jira.matching(http.MethodPost, "/transitions")
		require.Len(t, trs, 1)
		tr, _ := trs[0].Body["transition"].(map[string]any)
		require.Equal(t, "31", tr["id"])
	})

	t.Run("in_progress_is_left_alone", func(t *testing.T) {
		jira, srv := newJIRARecorder(t)
		jira.issueStatusCategory = "indeterminate"
		p := testPlugin(srv)
		meta := testMeta()
		meta["jira_url"] = srv.URL

		stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
		require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))
		require.Empty(t, jira.matching(http.MethodPost, "/transitions"),
			"a ticket someone is working must not be reset on every escalation")
	})

	t.Run("no_matching_transition_still_comments", func(t *testing.T) {
		jira, srv := newJIRARecorder(t)
		jira.issueStatusCategory = "done"
		jira.transitions = []map[string]any{
			{"id": "9", "name": "Archive", "to": map[string]any{"name": "Archived"}},
		}
		p := testPlugin(srv)
		meta := testMeta()
		meta["jira_url"] = srv.URL

		stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
		require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)),
			"a workflow with no reopen transition must not fail the notification")
		require.Len(t, jira.matching(http.MethodPost, "/comment"), 1)
		require.Empty(t, jira.matching(http.MethodPost, "/transitions"))
	})
}

func TestOnEscalationComment(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	jira.issueStatusCategory = "done"
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL
	meta["on_escalation"] = "comment"

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))

	require.Len(t, jira.matching(http.MethodPost, "/comment"), 1)
	require.Empty(t, jira.matching(http.MethodPost, "/transitions"),
		`on_escalation="comment" must not touch the ticket's status`)
}

func TestOnEscalationSkip(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL
	meta["on_escalation"] = "skip"

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))
	require.Empty(t, jira.calls(), `on_escalation="skip" must not call JIRA at all`)
}

func TestOnEscalationNewCreatesAndLinks(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL
	meta["on_escalation"] = "new"

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-9"}}
	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))

	require.Equal(t, 1, jira.creates())
	links := jira.matching(http.MethodPost, "/issueLink")
	require.Len(t, links, 1)
	outward, _ := links[0].Body["outwardIssue"].(map[string]any)
	require.Equal(t, "OPS-9", outward["key"], "the follow-up must link back to the alert's original ticket")
	linkType, _ := links[0].Body["type"].(map[string]any)
	require.Equal(t, "Relates", linkType["name"])

	// The anchor must not move, or a third escalation would chain off the
	// follow-up instead of the original.
	require.Equal(t, "OPS-9",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Create ticket", refIssueKey))
}

// A handle pointing at a ticket somebody deleted must not black-hole the alert.
func TestEscalationOnDeletedIssueFallsBackToCreate(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	jira.commentStatus = http.StatusNotFound
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-GONE"}}
	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))

	require.Equal(t, 1, jira.creates(), "a stale handle must fall back to a create")
	require.Equal(t, "OPS-1",
		plugins.NotifyRefString(snoozetypes.Record{Extra: stamped}, "Create ticket", refIssueKey),
		"the stale handle must be overwritten")
}

// A comment rejected for any other reason IS an error: the escalation did not
// reach JIRA and the dispatcher must record that.
func TestEscalationCommentErrorIsReported(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	jira.commentStatus = http.StatusForbidden
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
	err := p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped))
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
	require.Zero(t, jira.creates(), "a permissions problem must not be papered over with a new ticket")
}

// Two actions pointing at the same notifier keep independent tickets.
func TestHandlesAreScopedPerAction(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	// A handle exists for a DIFFERENT action, so this action must create.
	stamped := map[string]any{"notify_ref_Other action": map[string]any{refIssueKey: "OPS-5"}}
	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))
	require.Equal(t, 1, jira.creates())
}

// A record whose handle came from the legacy webhook inject_response must be
// honoured, so a deployment upgrading mid-incident does not duplicate.
func TestEscalationHonoursLegacyResponseHandle(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	stamped := map[string]any{"response_Create ticket": map[string]any{refIssueKey: "OPS-42"}}
	require.NoError(t, p.Send(context.Background(), recWithRef(stamped), escPayload(meta, 1, stamped)))

	require.Zero(t, jira.creates())
	comments := jira.matching(http.MethodPost, "/comment")
	require.Len(t, comments, 1)
	require.Contains(t, comments[0].Path, "OPS-42")
}

// Close stays a no-op: auto-close is the daemon's job.
func TestSendIgnoresClose(t *testing.T) {
	jira, srv := newJIRARecorder(t)
	p := testPlugin(srv)
	meta := testMeta()
	meta["jira_url"] = srv.URL

	stamped := map[string]any{"notify_ref_Create ticket": map[string]any{refIssueKey: "OPS-1"}}
	rec := recWithRef(stamped)
	rec.State = "close"
	require.NoError(t, p.Send(context.Background(), rec, escPayload(meta, 1, stamped)))
	require.Empty(t, jira.calls())
}

func TestOnEscalationRejectsUnknownValue(t *testing.T) {
	meta := testMeta()
	meta["jira_url"] = "https://example.atlassian.net"
	meta["on_escalation"] = "reopn"
	_, err := configFromMeta(meta)
	require.Error(t, err)
	require.Contains(t, err.Error(), "on_escalation")
}

func TestOnEscalationDefaultsToReopen(t *testing.T) {
	meta := testMeta()
	meta["jira_url"] = "https://example.atlassian.net"
	cfg, err := configFromMeta(meta)
	require.NoError(t, err)
	require.Equal(t, escalateReopen, cfg.OnEscalation)
	require.Equal(t, defaultReopenStatus, cfg.ReopenStatus)
	require.Equal(t, defaultLinkType, cfg.LinkType)
}

// adfPlainText flattens an ADF comment body back to text for assertions.
func adfPlainText(t *testing.T, body any) string {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	var doc struct {
		Content []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	var b strings.Builder
	for _, block := range doc.Content {
		for _, inline := range block.Content {
			b.WriteString(inline.Text)
		}
		b.WriteString("\n")
	}
	return b.String()
}
