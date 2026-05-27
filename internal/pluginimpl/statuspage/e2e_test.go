package statuspage

// End-to-end test against the real Atlassian Statuspage API.
//
// This test is skipped unless both SNOOZE_E2E_STATUSPAGE_API_KEY and
// SNOOZE_E2E_STATUSPAGE_PAGE_ID are set in the environment. It creates one
// incident and immediately resolves it, which is visible briefly in the
// Statuspage UI.
//
// See docs/content/general/integrations/statuspage.md for setup instructions.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestStatuspageE2E(t *testing.T) {
	apiKey := os.Getenv("SNOOZE_E2E_STATUSPAGE_API_KEY")
	pageID := os.Getenv("SNOOZE_E2E_STATUSPAGE_PAGE_ID")
	if apiKey == "" || pageID == "" {
		t.Skip("set SNOOZE_E2E_STATUSPAGE_API_KEY and SNOOZE_E2E_STATUSPAGE_PAGE_ID to run the Statuspage end-to-end test")
	}

	p, err := factory(plugins.Metadata{Name: "statuspage"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	meta := map[string]any{
		"api_key":        apiKey,
		"page_id":        pageID,
		"initial_status": "investigating",
		"name":           "e2e-test: snooze statuspage plugin",
		"body":           "Automated end-to-end test. Safe to ignore.",
		"timeout":        "15s",
	}

	rec := snoozetypes.Record{
		UID:      "e2e-1",
		Host:     "test-host",
		Source:   "snooze-e2e",
		Severity: "critical",
		Message:  "e2e test incident",
	}

	// Create the incident.
	t.Log("creating incident...")
	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "create incident must succeed")

	// Short pause so the Statuspage API propagates the incident before we
	// immediately look it up in the unresolved list.
	time.Sleep(2 * time.Second)

	// Resolve the incident.
	t.Log("resolving incident...")
	closeRec := rec
	closeRec.State = "close"
	err = p.(plugins.Notifier).Send(context.Background(), closeRec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "resolve incident must succeed")
}
