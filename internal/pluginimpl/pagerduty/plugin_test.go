package pagerduty

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "pagerduty"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)

	p, err := factory(plugins.Metadata{Name: "pagerduty"})
	require.NoError(t, err)
	require.Equal(t, "pagerduty", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// sampleRecord returns a canonical firing record for test reuse.
func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
		Hash:     "abc123",
	}
}

// newPluginForTest builds a Plugin whose http.Client is replaced with a
// plain one (no TLS plumbing) so it works with httptest.NewServer.
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "pagerduty"})
	require.NoError(t, err)
	pp := p.(*Plugin)
	pp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return pp
}

// capturedRequest holds the raw request data captured by a test server.
type capturedRequest struct {
	mu   sync.Mutex
	body []byte
}

func (c *capturedRequest) set(body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.body = body
}

func (c *capturedRequest) get() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := make([]byte, len(c.body))
	copy(b, c.body)
	return b
}

// pdEventCapture creates an httptest server that returns statusCode and
// captures the request body. The caller calls t.Cleanup via the server.
func pdEventCapture(t *testing.T, statusCode int) (*httptest.Server, *capturedRequest) {
	t.Helper()
	capt := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capt.set(body)
		w.Header().Set("Content-Type", "application/json")
		if statusCode == http.StatusAccepted {
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(`{"status":"success","dedup_key":"abc123"}`))
		} else {
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(`{"message":"bad request"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, capt
}

// baseMeta returns a minimum action_form meta map pointing at srv and using
// the given routing_key.
func baseMeta(apiBase, routingKey string) map[string]any {
	return map[string]any{
		"routing_key": routingKey,
		"api_base":    apiBase,
	}
}

// TestSendTrigger verifies that a standard firing alert is forwarded as a
// trigger event and that the payload fields are correctly populated.
func TestSendTrigger(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	})
	require.NoError(t, err)

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))

	require.Equal(t, "rk-test", evt.RoutingKey)
	require.Equal(t, "trigger", evt.EventAction)
	require.Equal(t, rec.Hash, evt.DedupKey, "dedup_key should prefer rec.Hash")
	require.Equal(t, "warning", evt.Payload.Severity, "snooze 'warning' → PD 'warning'")
	require.Contains(t, evt.Payload.Summary, rec.Host)
	require.Contains(t, evt.Payload.Summary, rec.Message)
	require.Equal(t, rec.Host, evt.Payload.Source)
}

// TestSendResolve verifies that a record with State=="close" is forwarded as a
// resolve event and carries the same dedup_key so PagerDuty closes the incident.
func TestSendResolve(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	})
	require.NoError(t, err)

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))

	require.Equal(t, "resolve", evt.EventAction)
	require.Equal(t, rec.Hash, evt.DedupKey, "resolve dedup_key must match trigger's")
}

// TestDedupKeyFallsBackToUID verifies that when rec.Hash is empty the plugin
// uses rec.UID as the dedup_key.
func TestDedupKeyFallsBackToUID(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Hash = "" // clear the hash
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	})
	require.NoError(t, err)

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))
	require.Equal(t, rec.UID, evt.DedupKey)
}

// TestSeverityMapping covers the Snooze → PagerDuty severity mapping table.
func TestSeverityMapping(t *testing.T) {
	cases := []struct {
		snooze  string
		wantPD  string
		resolve bool
	}{
		{"emergency", "critical", false},
		{"critical", "critical", false},
		{"error", "error", false},
		{"err", "error", false},
		{"warning", "warning", false},
		{"notice", "info", false},
		{"info", "info", false},
		{"debug", "info", false},
		{"unknown_thing", "critical", false}, // trigger default
		{"unknown_thing", "info", true},      // resolve default
	}

	for _, tc := range cases {
		t.Run(tc.snooze+"_resolve="+boolStr(tc.resolve), func(t *testing.T) {
			srv, capt := pdEventCapture(t, http.StatusAccepted)
			p := newPluginForTest(t)
			rec := sampleRecord()
			rec.Severity = tc.snooze
			if tc.resolve {
				rec.State = "close"
			}
			require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
				Meta: baseMeta(srv.URL, "rk-test"),
			}))
			var evt pdEvent
			require.NoError(t, json.Unmarshal(capt.get(), &evt))
			require.Equal(t, tc.wantPD, evt.Payload.Severity)
		})
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestSeverityOverride verifies that when severity is explicitly set to
// something other than "auto", the record's severity is not used.
func TestSeverityOverride(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Severity = "emergency" // would map to "critical" without override
	meta := baseMeta(srv.URL, "rk-test")
	meta["severity"] = "warning"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))
	require.Equal(t, "warning", evt.Payload.Severity, "explicit override must win")
}

// TestNon202Error verifies that a non-202 response from PagerDuty surfaces as
// an error whose message includes the status code and body excerpt.
func TestNon202Error(t *testing.T) {
	srv, _ := pdEventCapture(t, http.StatusBadRequest) // 400

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "400")
	require.Contains(t, err.Error(), "bad request")
}

// TestMissingRoutingKey verifies that a missing routing_key is caught early and
// does not produce an outbound HTTP request.
func TestMissingRoutingKey(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"api_base": "http://no-server.invalid",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "routing_key")
}

// TestMissingRoutingKeyNilMeta exercises the nil-meta path.
func TestMissingRoutingKeyNilMeta(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "routing_key")
}

// TestClientAndClientURL verifies that client/client_url from the action form
// are forwarded in the JSON body.
func TestClientAndClientURL(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL, "rk-test")
	meta["client"] = "MySnooze"
	meta["client_url"] = "https://snooze.example.com"
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))
	require.Equal(t, "MySnooze", evt.Client)
	require.Equal(t, "https://snooze.example.com", evt.ClientURL)
}

// TestSourceFallsBackToSource verifies that when rec.Host is empty,
// payload.source falls back to rec.Source.
func TestSourceFallsBackToSource(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Host = "" // force fallback
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	}))

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))
	require.Equal(t, rec.Source, evt.Payload.Source)
}

// TestCustomDetails verifies that the custom_details map carries the expected
// record fields.
func TestCustomDetails(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Tags = []string{"prod", "db"}
	rec.Raw = map[string]any{"extra_key": "extra_val"}
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	}))

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))
	require.Equal(t, rec.UID, evt.Payload.CustomDetails["uid"])
	require.Equal(t, rec.Hash, evt.Payload.CustomDetails["hash"])
	tags, ok := evt.Payload.CustomDetails["tags"].([]interface{})
	require.True(t, ok)
	require.Len(t, tags, 2)
}

// TestSummaryTruncation verifies that summary strings longer than 1024 runes
// are truncated before the request is sent.
func TestSummaryTruncation(t *testing.T) {
	srv, capt := pdEventCapture(t, http.StatusAccepted)

	p := newPluginForTest(t)
	rec := sampleRecord()
	// Build a message that will make the summary exceed 1024 runes.
	rec.Message = string(make([]rune, 1100))
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk-test"),
	}))

	var evt pdEvent
	require.NoError(t, json.Unmarshal(capt.get(), &evt))
	require.LessOrEqual(t, len([]rune(evt.Payload.Summary)), maxSummaryRunes)
}
