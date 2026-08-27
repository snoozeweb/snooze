package opsgenie

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

type ogReq struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

// ogRecorder records every Opsgenie call so tests can assert both what was
// called and — critically — that the alias never varies.
type ogRecorder struct {
	mu   sync.Mutex
	reqs []ogReq
	// failPaths maps a path suffix to the status it should answer with.
	failPaths map[string]int
}

func newOGRecorder(t *testing.T) (*ogRecorder, *httptest.Server) {
	t.Helper()
	r := &ogRecorder{failPaths: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body := map[string]any{}
		if raw, _ := io.ReadAll(req.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		r.mu.Lock()
		r.reqs = append(r.reqs, ogReq{Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery, Body: body})
		status := http.StatusAccepted
		for suffix, s := range r.failPaths {
			if strings.HasSuffix(req.URL.Path, suffix) {
				status = s
			}
		}
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return r, srv
}

func (r *ogRecorder) calls() []ogReq {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ogReq, len(r.reqs))
	copy(out, r.reqs)
	return out
}

func (r *ogRecorder) paths() []string {
	var out []string
	for _, c := range r.calls() {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

func (r *ogRecorder) find(t *testing.T, suffix string) ogReq {
	t.Helper()
	for _, c := range r.calls() {
		if strings.HasSuffix(c.Path, suffix) {
			return c
		}
	}
	t.Fatalf("no request ending in %q; calls=%v", suffix, r.paths())
	return ogReq{}
}

func (r *ogRecorder) has(suffix string) bool {
	for _, c := range r.calls() {
		if strings.HasSuffix(c.Path, suffix) {
			return true
		}
	}
	return false
}

// TestAliasIsIdenticalAcrossFirstFireAndEscalation is the single most important
// guard in the Opsgenie work: the alias is what makes Opsgenie treat an
// escalation as the same alert. Derive it from anything escalation-specific and
// one problem becomes a queue of identical alerts.
func TestAliasIsIdenticalAcrossFirstFireAndEscalation(t *testing.T) {
	og, srv := newOGRecorder(t)
	p := newPluginForTest(t)
	rec := sampleRecord()

	require.NoError(t, p.Send(context.Background(), rec,
		plugins.NotificationPayload{Meta: baseMeta(srv.URL)}))
	require.NoError(t, p.Send(context.Background(), rec,
		plugins.NotificationPayload{Meta: baseMeta(srv.URL), Escalation: plugins.Escalation{Count: 1}}))

	create := og.find(t, "/v2/alerts")
	alias, _ := create.Body["alias"].(string)
	require.Equal(t, rec.Hash, alias)

	// Every escalation call must address that same alias.
	for _, c := range og.calls() {
		if c.Path == "/v2/alerts" {
			continue
		}
		require.Contains(t, c.Path, "/v2/alerts/"+alias+"/",
			"escalation call %s must address the original alias", c.Path)
		require.Contains(t, c.Query, "identifierType=alias")
	}
}

// A re-escalation must never POST another create: Opsgenie would dedup it, but
// silently — nobody gets re-paged, which is the opposite of what an escalation
// is for.
func TestEscalationDoesNotRecreate(t *testing.T) {
	og, srv := newOGRecorder(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: baseMeta(srv.URL), Escalation: plugins.Escalation{Count: 2, Reason: "timeout"}}))

	for _, c := range og.calls() {
		require.NotEqual(t, "/v2/alerts", c.Path, "a re-escalation must not create a second alert")
	}
	require.True(t, og.has("/notes"))
	require.True(t, og.has("/unacknowledge"),
		"an acknowledged alert must start notifying again when it re-escalates")

	note, _ := og.find(t, "/notes").Body["note"].(string)
	require.Contains(t, note, "Re-escalated by Snooze #2")
	require.Contains(t, note, "(timeout)")
	require.Contains(t, note, "db-1.example.com")
}

// Priority is raised only when severity actually rose.
func TestEscalationPriorityOnlyWhenSeverityRose(t *testing.T) {
	t.Run("rose", func(t *testing.T) {
		og, srv := newOGRecorder(t)
		p := newPluginForTest(t)
		require.NoError(t, p.Send(context.Background(), sampleRecord(),
			plugins.NotificationPayload{
				Meta: baseMeta(srv.URL),
				// sampleRecord() is severity=warning; info -> warning is a rise.
				Escalation: plugins.Escalation{
					Count: 1, Severity: "warning", PreviousSeverity: "info",
				},
			}))
		req := og.find(t, "/priority")
		require.Equal(t, http.MethodPut, req.Method)
		require.NotEmpty(t, req.Body["priority"])
	})

	t.Run("flat", func(t *testing.T) {
		og, srv := newOGRecorder(t)
		p := newPluginForTest(t)
		require.NoError(t, p.Send(context.Background(), sampleRecord(),
			plugins.NotificationPayload{
				Meta: baseMeta(srv.URL),
				Escalation: plugins.Escalation{
					Count: 1, Severity: "warning", PreviousSeverity: "warning",
				},
			}))
		require.False(t, og.has("/priority"))
	})
}

// unack_on_escalation=false is honoured for teams who deliberately keep
// acknowledged alerts quiet.
func TestUnackCanBeDisabled(t *testing.T) {
	og, srv := newOGRecorder(t)
	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["unack_on_escalation"] = false

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: meta, Escalation: plugins.Escalation{Count: 1}}))

	require.True(t, og.has("/notes"))
	require.False(t, og.has("/unacknowledge"))
}

func TestUnackDefaultsOn(t *testing.T) {
	cfg, err := configFromMeta(baseMeta("https://api.opsgenie.com"))
	require.NoError(t, err)
	require.True(t, cfg.UnackOnEscalation)
}

// The note is the part that must land: its failure is reported. An
// unacknowledge rejection is not — an alert that was never acknowledged
// legitimately rejects it, and the escalation is already on the timeline.
func TestEscalationErrorHandling(t *testing.T) {
	t.Run("note_failure_is_reported", func(t *testing.T) {
		og, srv := newOGRecorder(t)
		og.failPaths["/notes"] = http.StatusForbidden
		p := newPluginForTest(t)

		err := p.Send(context.Background(), sampleRecord(),
			plugins.NotificationPayload{Meta: baseMeta(srv.URL), Escalation: plugins.Escalation{Count: 1}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "403")
	})

	t.Run("unack_failure_is_swallowed", func(t *testing.T) {
		og, srv := newOGRecorder(t)
		og.failPaths["/unacknowledge"] = http.StatusBadRequest
		p := newPluginForTest(t)

		require.NoError(t, p.Send(context.Background(), sampleRecord(),
			plugins.NotificationPayload{Meta: baseMeta(srv.URL), Escalation: plugins.Escalation{Count: 1}}),
			"an alert that was never acknowledged must not fail the notification")
	})

	t.Run("priority_failure_is_swallowed", func(t *testing.T) {
		og, srv := newOGRecorder(t)
		og.failPaths["/priority"] = http.StatusBadRequest
		p := newPluginForTest(t)

		require.NoError(t, p.Send(context.Background(), sampleRecord(),
			plugins.NotificationPayload{
				Meta: baseMeta(srv.URL),
				Escalation: plugins.Escalation{
					Count: 1, Severity: "warning", PreviousSeverity: "info",
				},
			}))
	})
}

// Close still closes by alias, unaffected by the escalation context.
func TestCloseStillClosesByAlias(t *testing.T) {
	og, srv := newOGRecorder(t)
	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"

	require.NoError(t, p.Send(context.Background(), rec,
		plugins.NotificationPayload{Meta: baseMeta(srv.URL), Escalation: plugins.Escalation{Count: 3}}))

	req := og.find(t, "/close")
	require.Contains(t, req.Path, rec.Hash)
	require.False(t, og.has("/notes"))
}

// A Switch value can be stored as a bool or as a string; both must disable the
// un-acknowledge, or an operator's setting is silently ignored.
func TestUnackAcceptsBoolAndStringForms(t *testing.T) {
	for name, v := range map[string]any{
		"bool":   false,
		"string": "false",
	} {
		t.Run(name, func(t *testing.T) {
			meta := baseMeta("https://api.opsgenie.com")
			meta["unack_on_escalation"] = v
			cfg, err := configFromMeta(meta)
			require.NoError(t, err)
			require.False(t, cfg.UnackOnEscalation)
		})
	}

	// An empty string is "unset", not "false": the default must survive.
	meta := baseMeta("https://api.opsgenie.com")
	meta["unack_on_escalation"] = ""
	cfg, err := configFromMeta(meta)
	require.NoError(t, err)
	require.True(t, cfg.UnackOnEscalation)
}
