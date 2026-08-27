package pagerduty

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// pdMultiCapture captures every event body posted, so a first fire and its
// escalations can be compared against each other.
type pdMultiCapture struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func newPDMultiCapture(t *testing.T) (*pdMultiCapture, *httptest.Server) {
	t.Helper()
	c := &pdMultiCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	t.Cleanup(srv.Close)
	return c, srv
}

func (c *pdMultiCapture) all() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]any, len(c.bodies))
	copy(out, c.bodies)
	return out
}

// TestDedupKeyIsIdenticalAcrossFirstFireAndEscalation is the load-bearing guard
// for PagerDuty: the dedup key is what makes an escalation land on the incident
// that already exists. Fold anything escalation-specific into it and one
// problem becomes a queue of identical incidents.
func TestDedupKeyIsIdenticalAcrossFirstFireAndEscalation(t *testing.T) {
	capt, srv := newPDMultiCapture(t)
	p := newPluginForTest(t)
	rec := sampleRecord()
	meta := baseMeta(srv.URL, "rk")

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}))
	for i := 1; i <= 3; i++ {
		require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
			Meta:       meta,
			Escalation: plugins.Escalation{Count: i, Reason: "timeout"},
		}))
	}

	bodies := capt.all()
	require.Len(t, bodies, 4)
	first := bodies[0]["dedup_key"]
	require.Equal(t, rec.Hash, first)
	for i, b := range bodies {
		require.Equal(t, first, b["dedup_key"], "event %d changed the dedup key", i)
		require.Equal(t, "trigger", b["event_action"])
	}
}

// The escalation context must reach responders and event rules.
func TestEscalationContextInCustomDetails(t *testing.T) {
	capt, srv := newPDMultiCapture(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv.URL, "rk"),
		Escalation: plugins.Escalation{
			Count: 3, Reason: "manual", Actor: "alice", PreviousSeverity: "info",
		},
	}))

	payload, _ := capt.all()[0]["payload"].(map[string]any)
	details, _ := payload["custom_details"].(map[string]any)
	require.EqualValues(t, 3, details["escalation_count"])
	require.Equal(t, "manual", details["escalation_reason"])
	require.Equal(t, "alice", details["escalated_by"])
	require.Equal(t, "info", details["previous_severity"])
}

// A first delivery must look exactly as it did before this change.
func TestFirstDeliveryHasNoEscalationDetails(t *testing.T) {
	capt, srv := newPDMultiCapture(t)
	p := newPluginForTest(t)

	require.NoError(t, p.Send(context.Background(), sampleRecord(),
		plugins.NotificationPayload{Meta: baseMeta(srv.URL, "rk")}))

	payload, _ := capt.all()[0]["payload"].(map[string]any)
	details, _ := payload["custom_details"].(map[string]any)
	for _, k := range []string{"escalation_count", "escalation_reason", "escalated_by"} {
		require.NotContains(t, details, k)
	}
}

// Resolve on close keeps the same dedup key, or PagerDuty would resolve nothing.
func TestResolveKeepsDedupKey(t *testing.T) {
	capt, srv := newPDMultiCapture(t)
	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta:       baseMeta(srv.URL, "rk"),
		Escalation: plugins.Escalation{Count: 2},
	}))

	body := capt.all()[0]
	require.Equal(t, "resolve", body["event_action"])
	require.Equal(t, rec.Hash, body["dedup_key"])
}

// A record that never went through aggregaterule has no hash; the uid fallback
// must still be stable across escalations.
func TestDedupKeyFallsBackToUIDStably(t *testing.T) {
	capt, srv := newPDMultiCapture(t)
	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.Hash = ""
	meta := baseMeta(srv.URL, "rk")

	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}))
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: meta, Escalation: plugins.Escalation{Count: 1},
	}))

	bodies := capt.all()
	require.Equal(t, rec.UID, bodies[0]["dedup_key"])
	require.Equal(t, bodies[0]["dedup_key"], bodies[1]["dedup_key"])
}
