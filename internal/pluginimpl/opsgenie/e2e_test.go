package opsgenie

// End-to-end test for the Opsgenie notifier plugin.
//
// Gate: the test is skipped unless SNOOZE_E2E_OPSGENIE_API_KEY is set.
// It performs one real create followed by one real close against the
// configured Opsgenie region. See docs/general/integrations/opsgenie.rst
// for setup instructions.

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestOpsgenieE2E(t *testing.T) {
	apiKey := os.Getenv("SNOOZE_E2E_OPSGENIE_API_KEY")
	if apiKey == "" {
		t.Skip("set SNOOZE_E2E_OPSGENIE_API_KEY to run the Opsgenie end-to-end test")
	}

	region := os.Getenv("SNOOZE_E2E_OPSGENIE_REGION") // optional; defaults to "us"
	if region == "" {
		region = "us"
	}

	// Build the real plugin (uses defaultClient and the real Opsgenie API).
	p, err := factory(plugins.Metadata{Name: "opsgenie"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	meta := map[string]any{
		"api_key": apiKey,
		"region":  region,
		"source":  "Snooze-E2E",
		"tags":    "e2e,snooze",
	}

	rec := snoozetypes.Record{
		UID:      "snooze-e2e-opsgenie-001",
		Hash:     "snooze-e2e-opsgenie-001",
		Host:     "e2e-test.snooze.internal",
		Source:   "Snooze-E2E",
		Severity: "warning",
		Message:  "Snooze Opsgenie e2e test — please ignore",
	}

	// Create the alert.
	t.Log("Creating Opsgenie alert ...")
	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "create alert should succeed")

	// Close the alert.
	t.Log("Closing Opsgenie alert ...")
	rec.State = "close"
	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "close alert should succeed")
}
