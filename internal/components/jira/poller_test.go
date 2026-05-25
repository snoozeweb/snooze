package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeSnoozeAPI captures POST calls so we can assert what the poller would
// send to snooze-server when a JIRA ticket transitions to Done.
type fakeSnoozeAPI struct {
	mu    sync.Mutex
	calls []fakeCall

	searchResp recordSearchEnvelope
}

type fakeCall struct {
	Path string
	Body any
}

func (f *fakeSnoozeAPI) Post(_ context.Context, path string, body, dest any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{Path: path, Body: body})
	if dest != nil {
		raw, _ := json.Marshal(f.searchResp)
		return json.Unmarshal(raw, dest)
	}
	return nil
}

func TestExtractHash_fromURL(t *testing.T) {
	hash := extractHash("https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dabc123")
	require.Equal(t, "abc123", hash)
}

func TestExtractHash_fromPlainEquals(t *testing.T) {
	require.Equal(t, "xyz", extractHash("hash=xyz"))
}

func TestExtractHash_fallbackVerbatim(t *testing.T) {
	require.Equal(t, "raw-hash", extractHash("raw-hash"))
}

func TestDefaultJQL_customfieldPrefix(t *testing.T) {
	require.Equal(t, "cf[10500] is not EMPTY AND statusCategory != Done",
		defaultJQL("customfield_10500"))
}

func TestDefaultJQL_quotedFallback(t *testing.T) {
	require.Equal(t, `"AlertHash" is not EMPTY AND statusCategory != Done`,
		defaultJQL("AlertHash"))
}

func TestPoller_cycleClosesDisappearedTickets(t *testing.T) {
	var jiraCalls atomic.Int32
	var openIssues atomic.Pointer[[]SearchIssue]

	open1 := []SearchIssue{
		{Key: "OPS-1", Fields: map[string]any{
			"customfield_10500": "https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dh1",
		}},
		{Key: "OPS-2", Fields: map[string]any{
			"customfield_10500": "https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dh2",
		}},
	}
	open2 := []SearchIssue{
		// OPS-1 still open, OPS-2 disappeared (== closed in JIRA).
		open1[0],
	}
	openIssues.Store(&open1)

	jira := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		jiraCalls.Add(1)
		require.Equal(t, "/rest/api/3/search/jql", r.URL.Path)
		resp := map[string]any{"issues": *openIssues.Load()}
		_ = json.NewEncoder(w).Encode(resp)
	})

	fake := &fakeSnoozeAPI{
		searchResp: recordSearchEnvelope{
			Data: []struct {
				UID string `json:"uid"`
			}{{UID: "rec-uid-2"}},
		},
	}

	cfg, _ := minimalCfg().WithDefaults()
	cfg.AlertHashCustomField = "customfield_10500"
	p := newPoller(cfg, jira, fake, nil)

	// First cycle: seed the tracked set.
	require.NoError(t, p.cycle(context.Background()))
	require.Empty(t, fake.calls, "first cycle should not close anything")

	// Swap to the smaller open set, run again — OPS-2 should be closed.
	openIssues.Store(&open2)
	require.NoError(t, p.cycle(context.Background()))

	require.NotEmpty(t, fake.calls)
	// First call should be the record search by hash.
	require.Equal(t, "/api/v1/record/search", fake.calls[0].Path)
	// Second call should be the close-comment.
	require.Equal(t, "/api/v1/comment", fake.calls[1].Path)
	payload, ok := fake.calls[1].Body.([]closeSnoozePayload)
	require.True(t, ok)
	require.Len(t, payload, 1)
	require.Equal(t, "close", payload[0].Type)
	require.Equal(t, "rec-uid-2", payload[0].RecordUID)
}
