package googlechat

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestGoogleChatE2E sends a real message to a Google Chat space via an
// Incoming Webhook. The test is skipped when SNOOZE_E2E_GOOGLECHAT_WEBHOOK
// is not set, so it never runs in CI by default.
//
// To run:
//
//	export SNOOZE_E2E_GOOGLECHAT_WEBHOOK="https://chat.googleapis.com/v1/spaces/.../messages?key=...&token=..."
//	go test -run E2E ./internal/pluginimpl/googlechat/...
func TestGoogleChatE2E(t *testing.T) {
	webhookURL := os.Getenv("SNOOZE_E2E_GOOGLECHAT_WEBHOOK")
	if webhookURL == "" {
		t.Skip("set SNOOZE_E2E_GOOGLECHAT_WEBHOOK to run the Google Chat end-to-end test")
	}

	rawPlugin, err := factory(plugins.Metadata{Name: "googlechat"})
	require.NoError(t, err)
	require.NoError(t, rawPlugin.PostInit(context.Background(), nil))
	p, ok := rawPlugin.(plugins.Notifier)
	require.True(t, ok, "plugin must implement plugins.Notifier")

	rec := snoozetypes.Record{
		UID:       "e2e-test-1",
		Host:      "test-host.example.com",
		Source:    "snooze-e2e",
		Severity:  "info",
		Message:   "Google Chat notifier e2e test — please ignore",
		Hash:      "e2e-hash",
		Timestamp: time.Now(),
	}

	// Send once as a card (default).
	err = p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": webhookURL,
			"use_card":    true,
		},
	})
	require.NoError(t, err)

	// Send once as plain text.
	err = p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": webhookURL,
			"use_card":    false,
			"message":     "Plain text e2e: *{{ .Severity }}* on {{ .Host }}",
		},
	})
	require.NoError(t, err)
}
