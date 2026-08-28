package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/pluginimpl/webhook"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestRoundTrip_secondEscalationCommentsNotCreates closes the loop the two
// halves of the duplicate-ticket fix live on: the webhook notifier in
// snooze-server, and this daemon's /alert endpoint. It drives the real
// notifier at the real HTTP handler twice for one alert — exactly what a
// re-escalation does — with the Inject callback writing back onto the record
// the way the notification worker does.
//
// Production, before the fix: two deliveries, two tickets (CG-1811 at
// 02:11:36 and CG-1812 at 02:13:50 for alert hash ec2f9135…). Expected: one
// create, then one comment on that same issue.
func TestRoundTrip_secondEscalationCommentsNotCreates(t *testing.T) {
	var (
		mu       sync.Mutex
		creates  int
		comments []string
	)
	jira := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			creates++
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "CG-1811"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment"):
			comments = append(comments,
				strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/"), "/")[0])
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected JIRA call: %s %s", r.Method, r.URL.Path)
		}
	})

	cfg, _ := minimalCfg().WithDefaults()
	daemon := httptest.NewServer(http.HandlerFunc(
		newHTTPServer("127.0.0.1:0", newForwarder(cfg, jira, nil), nil).handleAlert))
	t.Cleanup(daemon.Close)

	rec := snoozetypes.Record{
		Hash:     "ec2f9135df5035c0fc6414996b6f8b42",
		Host:     "K8S ovh",
		Severity: "critical",
		Message:  "Deployment down: kube-system/node-overprovisioner (ovh)",
		Extra:    map[string]any{},
	}
	// The production action: batch and inject_response both on (batching is
	// disabled by inject_response), body carrying the whole record under
	// "alert" via the 1.x idiom.
	meta := map[string]any{
		"url":             daemon.URL + "/alert",
		"body":            `{"project_key": "CG", "alert": {{ __self__ | tojson() }}}`,
		"inject_response": true,
		"action_name":     "Jira Ticket",
		"batch":           true,
		"batch_maxsize":   100,
		"batch_timer":     10,
	}

	notifier := &webhook.Plugin{}
	require.NoError(t, notifier.PostInit(context.Background(), nil))
	for i := 0; i < 2; i++ {
		payload := plugins.NotificationPayload{
			Meta: meta,
			// The notification worker persists injected fields onto the
			// record; the next delivery renders from the updated record.
			Inject: func(field string, value any) { rec.Extra[field] = value },
		}
		require.NoError(t, notifier.Send(context.Background(), rec, payload),
			"delivery %d", i+1)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, creates, "the second escalation must not open a second ticket")
	require.Equal(t, []string{"CG-1811"}, comments,
		"the second escalation must comment on the ticket the first one opened")
	require.Equal(t, map[string]any{"issue_key": "CG-1811"}, rec.Extra["response_Jira Ticket"],
		"the handle must be stored where every reader looks for it")
}
