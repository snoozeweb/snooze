package slack

// End-to-end tests for the Slack notifier. These tests are skipped unless the
// relevant environment variables are set; they are safe to run as part of
// `go test ./...` because `t.Skip` fires at the top of each test.
//
// To run them, export the required variables (see docs/general/integrations/slack.rst)
// and then:
//
//	go test -v -run E2E ./internal/pluginimpl/slack/...

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestSlackE2E exercises the Slack notifier against the real Slack API.
//
// Required environment variable (Incoming Webhook mode):
//
//	SNOOZE_E2E_SLACK_WEBHOOK — e.g. https://hooks.slack.com/services/T.../B.../...
//
// Optional environment variables (Bot Token mode — set both to exercise that path):
//
//	SNOOZE_E2E_SLACK_BOT_TOKEN — e.g. xoxb-...
//	SNOOZE_E2E_SLACK_CHANNEL   — e.g. C01234ABCDE or #alerts
func TestSlackE2E(t *testing.T) {
	webhookURL := os.Getenv("SNOOZE_E2E_SLACK_WEBHOOK")
	botToken := os.Getenv("SNOOZE_E2E_SLACK_BOT_TOKEN")
	channel := os.Getenv("SNOOZE_E2E_SLACK_CHANNEL")

	if webhookURL == "" && botToken == "" {
		t.Skip("set SNOOZE_E2E_SLACK_WEBHOOK (or SNOOZE_E2E_SLACK_BOT_TOKEN + SNOOZE_E2E_SLACK_CHANNEL) to run the Slack end-to-end test")
	}

	p, err := factory(plugins.Metadata{Name: "slack"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	rec := snoozetypes.Record{
		UID:      "e2e-test-1",
		Host:     "test-host",
		Source:   "snooze-e2e",
		Severity: "info",
		Message:  "Snooze Slack integration end-to-end test",
		State:    "",
	}

	meta := map[string]any{
		"timeout": (15 * time.Second).String(),
	}
	if webhookURL != "" {
		meta["webhook_url"] = webhookURL
	}
	if botToken != "" {
		meta["bot_token"] = botToken
		meta["channel"] = channel
	}

	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	// Also test the resolved path when a webhook URL is available.
	if webhookURL != "" {
		rec.State = "close"
		rec.Message = "Snooze Slack integration end-to-end test (resolved)"
		err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
		require.NoError(t, err)
	}
}
