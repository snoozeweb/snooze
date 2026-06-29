package schema

import "testing"

// TestDefaultChatSchemas asserts the canonical defaults of the chat-interactive
// receiver file-config sections: Telegram's APIBase points at the public Bot API
// endpoint, and every secret field is zero-valued (fail-closed: an unset secret
// makes the receiver reject every request).
func TestDefaultChatSchemas(t *testing.T) {
	tg := DefaultTelegramInteractive()
	if tg.APIBase != "https://api.telegram.org" {
		t.Errorf("TelegramInteractive.APIBase = %q, want %q", tg.APIBase, "https://api.telegram.org")
	}
	if tg.BotToken != "" {
		t.Errorf("TelegramInteractive.BotToken = %q, want empty", tg.BotToken)
	}
	if tg.SecretToken != "" {
		t.Errorf("TelegramInteractive.SecretToken = %q, want empty", tg.SecretToken)
	}

	sl := DefaultSlackInteractive()
	if sl.SigningSecret != "" {
		t.Errorf("SlackInteractive.SigningSecret = %q, want empty", sl.SigningSecret)
	}
}
