// This file holds the cross-plugin escalation contract test.
//
// Every per-plugin escalate_test.go asserts the specifics: that JIRA comments
// instead of creating, that Opsgenie keeps its alias, that mail sets
// In-Reply-To. This test asserts the one thing they cannot: that a notifier
// reacts to a re-escalation AT ALL.
//
// The failure it exists to catch is a new notifier — or a refactor of an old
// one — that quietly ignores payload.Escalation and re-runs its create path,
// which is exactly the bug this whole feature set fixed. That regression is
// invisible in a per-plugin test suite that nobody remembered to write.

package all

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// escalationFixture is the escalation context every case is driven with.
//
// The severity pair is REAL — the values aggregaterule actually stamps — because
// this suite's original fixtures invented a `trend_indication` spelling ("up")
// that the pipeline never produces, which hid a bug where the priority bump in
// three notifiers silently never fired. Fixtures here must mirror the pipeline,
// not the author's expectation of it. The trend label is deliberately set to the
// genuine Alerta spelling so nothing can quietly depend on a made-up one.
func escalationFixture() plugins.Escalation {
	return plugins.Escalation{
		Count:  2,
		Reason: "timeout",
		// Severity MUST equal the record's own severity (see driveNotifier), and
		// the rise is expressed by a LOWER PreviousSeverity. If the record's
		// severity varied between the two runs the requests would differ for
		// that reason alone, and this test would pass for a notifier that
		// ignores the escalation completely.
		Severity:         fixtureSeverity,
		PreviousSeverity: "info",
		Trend:            "moreSevere",
	}
}

// fixtureSeverity is the severity used on BOTH runs, so severity is never the
// thing that makes the two requests differ.
const fixtureSeverity = "warning"

// escalationCase describes how to drive one notifier against a local HTTP
// stand-in.
type escalationCase struct {
	// meta builds the action config, pointed at the test server.
	meta func(serverURL string) map[string]any
	// handler, when set, scripts the stand-in's responses. The default answers
	// 200 with an empty JSON object.
	handler http.HandlerFunc
}

// escalationCases covers every notifier that can be driven over plain HTTP.
// Notifiers that cannot are listed in notHTTPDrivable below, with the reason —
// a silent omission here would let the guard rot into a no-op.
var escalationCases = map[string]escalationCase{
	"discord":    {meta: func(u string) map[string]any { return map[string]any{"webhook_url": u} }},
	"googlechat": {meta: func(u string) map[string]any { return map[string]any{"webhook_url": u} }},
	"mattermost": {meta: func(u string) map[string]any { return map[string]any{"webhook_url": u} }},
	"teams":      {meta: func(u string) map[string]any { return map[string]any{"webhook_url": u} }},
	"slack":      {meta: func(u string) map[string]any { return map[string]any{"webhook_url": u} }},
	"webhook":    {meta: func(u string) map[string]any { return map[string]any{"url": u} }},
	"ntfy": {meta: func(u string) map[string]any {
		return map[string]any{"server": u, "topic": "alerts"}
	}},
	"pushover": {meta: func(u string) map[string]any {
		return map[string]any{"token": "tok", "user": "usr", "api_base": u}
	}},
	"pagerduty": {
		meta: func(u string) map[string]any {
			return map[string]any{"routing_key": "rk", "api_base": u}
		},
		handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		},
	},
	"telegram": {
		meta: func(u string) map[string]any {
			return map[string]any{
				"bot_token": "123456789:ABCDefGhIJKlmNoPQRstuVwxYZ",
				"chat_id":   "-100987654321",
				"api_base":  u,
			}
		},
		handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
		},
	},
	"twilio": {
		meta: func(u string) map[string]any {
			return map[string]any{
				"account_sid": "AC00000000000000000000000000000000",
				"auth_token":  "tok",
				"from":        "+15550000000",
				"to":          "+15551111111",
				"api_base":    u,
			}
		},
	},
	"opsgenie": {meta: func(u string) map[string]any {
		return map[string]any{"api_key": "k", "api_base": u}
	}},
	"servicenow": {
		meta: func(u string) map[string]any {
			return map[string]any{"instance_url": u, "username": "u", "password": "p"}
		},
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				// Report an existing incident so the escalation path takes its
				// PATCH branch rather than falling back to a create.
				_, _ = w.Write([]byte(`{"result":[{"sys_id":"SYS1","state":"2"}]}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"result":{"sys_id":"SYS1"}}`))
		},
	},
	"statuspage": {
		meta: func(u string) map[string]any {
			return map[string]any{"api_key": "k", "page_id": "p", "api_base": u}
		},
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[{"id":"inc-1","name":"warning on db-1","status":"investigating"}]`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"inc-1"}`))
		},
	},
	"jira": {
		meta: func(u string) map[string]any {
			return map[string]any{
				"jira_url": u, "email": "b@x.io", "api_token": "t", "project_key": "OPS",
			}
		},
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.URL.Path == "/rest/api/3/priority":
				_, _ = w.Write([]byte(`[{"id":"1","name":"Highest"},{"id":"2","name":"High"}]`))
			case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"key":"OPS-1"}`))
			default:
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{}`))
			}
		},
	},
	"patlite": {meta: func(u string) map[string]any {
		return map[string]any{"host": u, "severity_map": map[string]any{
			"default": map[string]any{"color": "red", "state": "on"},
		}}
	}},
	"snoozepeer": {meta: func(u string) map[string]any { return map[string]any{"endpoint": u} }},
}

// notHTTPDrivable records the notifiers this test deliberately does not cover,
// and why. Each is covered by its own package's escalate_test.go instead.
var notHTTPDrivable = map[string]string{
	"mail":   "speaks SMTP, not HTTP; covered by mail/escalate_test.go",
	"script": "executes a local process; covered by script/escalate_test.go",
	"sns":    "requires SigV4 credentials; covered by sns/escalate_test.go",
}

// TestEveryNotifierReactsToReEscalation drives each notifier twice — once as a
// first delivery, once as a re-escalation — and requires the two outbound
// requests to differ. A notifier that ignores the escalation produces identical
// bytes, which is precisely the duplicate-ticket / duplicate-message bug.
func TestEveryNotifierReactsToReEscalation(t *testing.T) {
	for name, tc := range escalationCases {
		t.Run(name, func(t *testing.T) {
			notifier := notifierByName(t, name)

			first := driveNotifier(t, notifier, tc, plugins.Escalation{})
			// Drive the first delivery TWICE. Without this the comparison below
			// is worthless the moment a notifier puts anything time- or
			// random-varying in its payload: the two runs would differ for that
			// reason and the test would pass while the notifier ignored the
			// escalation entirely.
			firstAgain := driveNotifier(t, notifier, tc, plugins.Escalation{})
			require.Equal(t, first, firstAgain,
				"%s produces a non-deterministic request, so this test cannot tell "+
					"an escalation apart from noise. Make the payload deterministic for "+
					"a fixed record, or compare a stable projection of it here.", name)

			escalated := driveNotifier(t, notifier, tc, escalationFixture())

			require.NotEmpty(t, first, "the first delivery made no request at all")
			require.NotEmpty(t, escalated, "the escalation made no request at all")
			require.NotEqual(t, first, escalated,
				"%s produced byte-identical requests for a first delivery and a "+
					"re-escalation, which means it ignores payload.Escalation. Either "+
					"branch on it (update the existing object / thread the reply / raise "+
					"the urgency) or, if that is genuinely impossible for this transport, "+
					"say so here and in docs/content/general/notifications/escalation.md.", name)
		})
	}
}

// TestEveryNotifierIsAccountedFor fails when a new notifier is registered
// without either a case above or an explicit exclusion. Without it this guard
// would quietly stop covering new plugins.
func TestEveryNotifierIsAccountedFor(t *testing.T) {
	for _, name := range plugins.Registered() {
		plug, err := plugins.New(name)
		require.NoError(t, err)
		if _, ok := plug.(plugins.Notifier); !ok {
			continue
		}
		_, covered := escalationCases[name]
		_, excluded := notHTTPDrivable[name]
		require.True(t, covered || excluded,
			"notifier %q has no escalation contract case and no documented exclusion. "+
				"Add it to escalationCases, or to notHTTPDrivable with the reason.", name)
	}
}

// notifierByName builds the named plugin and asserts it is a Notifier.
func notifierByName(t *testing.T, name string) plugins.Notifier {
	t.Helper()
	plug, err := plugins.New(name)
	require.NoError(t, err)
	n, ok := plug.(plugins.Notifier)
	require.True(t, ok, "%s is not a Notifier", name)
	return n
}

// bytesReader re-wraps a consumed request body so a scripted handler can read
// it again.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// driveNotifier sends one record at the given escalation and returns every
// request body the stand-in received, concatenated with its method and path so
// a change of verb or endpoint counts as a difference too.
func driveNotifier(t *testing.T, notifier plugins.Notifier, tc escalationCase, esc plugins.Escalation) string {
	t.Helper()

	var mu sync.Mutex
	var seen string
	handler := tc.handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen += r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery + "\n" + string(raw) + "\n"
		mu.Unlock()
		if handler != nil {
			r.Body = io.NopCloser(bytesReader(raw))
			handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	meta := tc.meta(srv.URL)
	meta["action_name"] = "Contract"
	meta["notification_name"] = "Contract rule"

	rec := snoozetypes.Record{
		UID: "rec-1", Hash: "abc123", Host: "db-1",
		Source: "syslog", Severity: fixtureSeverity, Message: "disk full",
		// The record and the payload must agree: the dispatcher derives
		// payload.Escalation FROM the record (plugins.EscalationFrom), so a
		// fixture that sets only one of them would be testing a state that
		// cannot occur — and would let a record-forwarding notifier
		// (webhook, snoozepeer) look broken when it is not.
		EscalationCount:  esc.Count,
		EscalationReason: esc.Reason,
		EscalationActor:  esc.Actor,
	}
	// The handle a first delivery would have stored, so the escalation run can
	// take the update path rather than falling back to a create.
	rec.Extra = map[string]any{
		"notify_ref_Contract": map[string]any{
			"issue_key":  "OPS-1",
			"thread_ts":  "1700000000.000100",
			"message_id": int64(42),
		},
		"previous_severity": esc.PreviousSeverity,
		"trend_indication":  esc.Trend,
	}

	// A notifier failure is not the subject of this test — some stand-in
	// responses are deliberately minimal — but it must still have made requests,
	// which the caller asserts.
	_ = notifier.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:       meta,
		Escalation: esc,
	})

	mu.Lock()
	defer mu.Unlock()
	return seen
}
