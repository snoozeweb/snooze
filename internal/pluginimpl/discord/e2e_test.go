package discord

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestDiscordE2E sends a real alert message to a Discord channel via an
// Incoming Webhook. The test is skipped unless SNOOZE_E2E_DISCORD_WEBHOOK is
// set to a valid webhook URL.
//
// To run:
//
//	export SNOOZE_E2E_DISCORD_WEBHOOK="https://discord.com/api/webhooks/<id>/<token>"
//	go test -run TestDiscordE2E ./internal/pluginimpl/discord/...
func TestDiscordE2E(t *testing.T) {
	webhookURL := os.Getenv("SNOOZE_E2E_DISCORD_WEBHOOK")
	if webhookURL == "" {
		t.Skip("set SNOOZE_E2E_DISCORD_WEBHOOK to run the Discord end-to-end test")
	}

	p, err := factory(plugins.Metadata{Name: "discord"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	rec := snoozetypes.Record{
		UID:       "e2e-test-1",
		Host:      "e2e-host.example.com",
		Source:    "snooze-e2e",
		Severity:  "warning",
		Message:   "End-to-end test from snooze discord plugin",
		Timestamp: time.Now().UTC(),
	}

	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": webhookURL,
			"username":    "Snooze E2E",
			"use_embed":   true,
		},
	})
	require.NoError(t, err)
}
