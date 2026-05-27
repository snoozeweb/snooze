package telegram

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestTelegramE2E sends one real Telegram message and asserts no error.
//
// Prerequisites — set these env vars before running:
//
//	SNOOZE_E2E_TELEGRAM_BOT_TOKEN  bot token obtained from @BotFather
//	SNOOZE_E2E_TELEGRAM_CHAT_ID    ID of the chat the bot has access to
//
// The test is skipped automatically when either variable is unset, so
// `go test ./internal/pluginimpl/telegram/...` stays green in CI without
// any Telegram credentials.
func TestTelegramE2E(t *testing.T) {
	botToken := os.Getenv("SNOOZE_E2E_TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("SNOOZE_E2E_TELEGRAM_CHAT_ID")
	if botToken == "" || chatID == "" {
		t.Skip("set SNOOZE_E2E_TELEGRAM_BOT_TOKEN and SNOOZE_E2E_TELEGRAM_CHAT_ID to run the Telegram end-to-end test")
	}

	// Use the real defaultClient so the full HTTP stack is exercised.
	p, err := factory(plugins.Metadata{Name: "telegram"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	rec := snoozetypes.Record{
		UID:      "e2e-test-1",
		Host:     "test-host.example.com",
		Source:   "snooze-e2e",
		Severity: "info",
		Message:  "Snooze Telegram integration end-to-end test",
	}

	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"bot_token": botToken,
			"chat_id":   chatID,
		},
	})
	require.NoError(t, err)
}
