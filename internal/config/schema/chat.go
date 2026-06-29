package schema

// chat.go holds the file-config sections for the inbound chat-interactive
// webhook receivers (Plan 36): Slack message buttons and Telegram inline
// keyboards that let an operator ack/close/re-open a record straight from a
// chat message.
//
// These are file-config (not runtime-config) by design: they carry deploy-time
// credentials (a Slack signing secret, a Telegram bot token + secret token),
// the same tier as ingest.token — not API-editable. The receivers read them via
// host.Config().
//
// Fail-closed: an unset secret makes the corresponding receiver reject every
// request (401). A public, unauthenticated state-mutation endpoint is
// unacceptable, so there are NO required-when-enabled checks — an empty secret is
// simply "off", and the receiver refuses everything until one is set.

// SlackInteractive configures the Slack interactive-message webhook receiver
// (WebhookPath "/slack"). SigningSecret is the Slack app's signing secret, used
// to verify the X-Slack-Signature HMAC on every inbound request. Empty ⇒ the
// receiver 401s every request.
type SlackInteractive struct {
	SigningSecret string `koanf:"signing_secret"`
}

// TelegramInteractive configures the Telegram callback-query webhook receiver
// (WebhookPath "/telegram").
//
//   - BotToken is the bot token (from @BotFather) used to call editMessageText /
//     answerCallbackQuery so the chat message can be edited in place.
//   - SecretToken is matched (constant-time) against the
//     X-Telegram-Bot-Api-Secret-Token header Telegram sets when the webhook was
//     registered with setWebhook. Empty ⇒ the receiver 401s every request.
//   - APIBase is the Bot API base URL; override to point at a self-hosted
//     instance. Defaults to https://api.telegram.org.
type TelegramInteractive struct {
	BotToken    string `koanf:"bot_token"`
	SecretToken string `koanf:"secret_token"`
	APIBase     string `koanf:"api_base"`
}

// DefaultSlackInteractive returns the canonical defaults: no signing secret, so
// the receiver is fail-closed (rejects every request) until one is configured.
func DefaultSlackInteractive() SlackInteractive {
	return SlackInteractive{}
}

// DefaultTelegramInteractive returns the canonical defaults: the public Telegram
// Bot API base, no bot/secret token (so the receiver is fail-closed until both
// are configured).
func DefaultTelegramInteractive() TelegramInteractive {
	return TelegramInteractive{
		APIBase: "https://api.telegram.org",
	}
}
