package pagerduty

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestPagerDutyE2E triggers and then resolves a real PagerDuty incident via
// the Events API v2. It is skipped automatically when
// SNOOZE_E2E_PAGERDUTY_ROUTING_KEY is not set so that `go test ./...` stays
// green in CI.
//
// To run the test, create an Events API v2 service integration in your
// PagerDuty account, copy the integration key (also called routing key), then:
//
//	export SNOOZE_E2E_PAGERDUTY_ROUTING_KEY="<your-integration-key>"
//	go test -run TestPagerDutyE2E ./internal/pluginimpl/pagerduty/...
func TestPagerDutyE2E(t *testing.T) {
	routingKey := os.Getenv("SNOOZE_E2E_PAGERDUTY_ROUTING_KEY")
	if routingKey == "" {
		t.Skip("set SNOOZE_E2E_PAGERDUTY_ROUTING_KEY to run the PagerDuty end-to-end test")
	}

	// Use the real defaultClient (no httptest override).
	p, err := factory(plugins.Metadata{Name: "pagerduty"})
	require.NoError(t, err)
	pp := p.(*Plugin)
	require.NoError(t, pp.PostInit(context.Background(), nil))

	meta := map[string]any{
		"routing_key": routingKey,
		"client":      "Snooze E2E Test",
		"timeout":     "15s",
	}

	rec := snoozetypes.Record{
		UID:       "e2e-pd-" + time.Now().UTC().Format("20060102T150405"),
		Host:      "test-host.e2e.local",
		Source:    "snooze-e2e",
		Severity:  "warning",
		Message:   "E2E test event — will be resolved immediately",
		Hash:      "snooze-e2e-test-dedupkey",
		Timestamp: time.Now().UTC(),
	}

	// Step 1: trigger.
	err = pp.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "trigger event must succeed")

	// Brief pause so PagerDuty processes the trigger before the resolve.
	time.Sleep(2 * time.Second)

	// Step 2: resolve via the same dedup_key.
	rec.State = "close"
	err = pp.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "resolve event must succeed")
}
