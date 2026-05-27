package pushover

// End-to-end test for the Pushover notifier.
//
// This file sends a real push notification via the Pushover API. It is skipped
// by default so that `go test ./...` stays green in CI environments that have
// no Pushover credentials.
//
// To run the test, export the required environment variables and pass
// -run E2E:
//
//	export SNOOZE_E2E_PUSHOVER_TOKEN="<your-app-token>"
//	export SNOOZE_E2E_PUSHOVER_USER="<your-user-or-group-key>"
//	go test -run E2E ./internal/pluginimpl/pushover/...

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestPushoverE2E delivers a real push notification to the Pushover API. It
// is gated on the SNOOZE_E2E_PUSHOVER_TOKEN and SNOOZE_E2E_PUSHOVER_USER
// environment variables and skipped when either is absent.
func TestPushoverE2E(t *testing.T) {
	token := os.Getenv("SNOOZE_E2E_PUSHOVER_TOKEN")
	user := os.Getenv("SNOOZE_E2E_PUSHOVER_USER")
	if token == "" || user == "" {
		t.Skip("set SNOOZE_E2E_PUSHOVER_TOKEN and SNOOZE_E2E_PUSHOVER_USER to run the Pushover end-to-end test")
	}

	raw, err := factory(plugins.Metadata{Name: "pushover"})
	require.NoError(t, err)
	require.NoError(t, raw.PostInit(context.Background(), nil))
	p := raw.(plugins.Notifier)

	rec := snoozetypes.Record{
		UID:      "e2e-test-1",
		Host:     "ci.example.com",
		Source:   "snooze-e2e",
		Severity: "info",
		Message:  "Snooze Pushover plugin end-to-end test — please ignore",
	}

	err = p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"token":    token,
			"user":     user,
			"title":    "Snooze E2E Test",
			"message":  "{{ .Message }} (host={{ .Host }})",
			"priority": "auto",
			"sound":    "none",
		},
	})
	require.NoError(t, err)
}
