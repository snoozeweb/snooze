---
sidebar_position: 24
---

# Telegram (output)

## Overview

The **Telegram** notifier is an in-process output plugin that posts alert messages to a Telegram chat using the [Telegram Bot API](https://core.telegram.org/bots/api) (`sendMessage` method). It requires a Telegram bot token and a target `chat_id` (which can be a private chat, group, supergroup, or channel the bot has been added to). All outbound HTTP traffic goes through `net/http`; no SDK dependency is added.

## Configuration

Wire the plugin to a notification by selecting **Send a Telegram message** as the action in the notification configuration UI, then fill in the action form fields described below.

### Field reference

``` yaml
bot_token: "123456789:ABCDefGhIJKlmNoPQRstuVwxYZ"   # from @BotFather
chat_id: "-100987654321"                              # group/channel ID
message: "<b>{{ .Severity | htmlEscape }}</b> on {{ .Host | htmlEscape }}\n{{ .Message | htmlEscape }}"
parse_mode: "HTML"          # HTML | MarkdownV2 | none
disable_notification: false # true → silent message
api_base: "https://api.telegram.org"  # override for a self-hosted Bot API
timeout: "10s"
```

| Field | Type | Required | Description |
|----|----|----|----|
| `bot_token` | Password | Yes | Telegram bot token issued by `@BotFather`, e.g. `123456789:ABC…`. |
| `chat_id` | String | Yes | Numeric Telegram chat identifier. Use a positive integer for a private chat with a user, a negative integer (`-100…`) for a supergroup or channel. Obtain it via `@userinfobot` or the `getUpdates` API call described below. |
| `message` | Text (Go template) | No | Message body rendered as a Go `text/template` over the `snoozetypes.Record`. Available fields: `.UID`, `.Host`, `.Source`, `.Process`, `.Severity`, `.Message`, `.Timestamp`, etc. The `htmlEscape` template function HTML-escapes a string (safe to use with `parse_mode: HTML`). Default: `<b>{{ .Severity | htmlEscape }}</b> on {{ .Host | htmlEscape }}\n{{ .Message | htmlEscape }}`. |
| `parse_mode` | Selector | No | Telegram message formatting. `HTML` (default) enables a safe subset of HTML tags; `MarkdownV2` uses Telegram's extended Markdown syntax; `none` sends plain text. |
| `disable_notification` | Switch | No | When `true`, the message is delivered silently — the recipient device shows no notification sound or banner. Default: `false`. |
| `api_base` | String | No | Base URL of the Bot API server. Default: `https://api.telegram.org`. Override to point at a [self-hosted Bot API server](https://core.telegram.org/bots/api#using-a-local-bot-api-server). |
| `timeout` | String | No | HTTP request timeout as a Go duration string (e.g. `10s`, `30s`). Default: `10s`. |

### HTML parse mode tips

When `parse_mode` is `HTML`, Telegram accepts only a [limited subset of HTML tags](https://core.telegram.org/bots/api#html-style): `<b>`, `<i>`, `<u>`, `<s>`, `<code>`, `<pre>`, `<a href="…">`. Always HTML-escape dynamic record fields with the `htmlEscape` template function to avoid message delivery failures caused by stray `<` or `&` characters in hostnames or log messages.

## End-to-end test setup

The e2e test in `internal/pluginimpl/telegram/e2e_test.go` sends one real Telegram message to verify the full integration. It is **skipped by default** unless both env vars below are set.

### Step 1 — Create a bot

1.  Open Telegram and start a conversation with `@BotFather`.
2.  Send `/newbot` and follow the prompts to choose a name and username.
3.  Copy the bot token that `@BotFather` issues (format: `<numeric_id>:<random_string>`).

### Step 2 — Obtain the chat ID

**For a group or supergroup:**

1.  Add the bot to the group.
2.  Send any message in the group.
3.  Fetch `https://api.telegram.org/bot<TOKEN>/getUpdates` in a browser or with `curl`.
4.  The `chat.id` field in the result (a negative number like `-1001234567890`) is the `chat_id`.

**For a private chat:**

1.  Start a conversation with the bot (send `/start`).
2.  Fetch `getUpdates` as above; `chat.id` is a positive integer.

**Using** `@userinfobot`:

Forward a message from the target chat to `@userinfobot`; it replies with the chat's numeric ID.

### Step 3 — Run the e2e test

``` console
$ export SNOOZE_E2E_TELEGRAM_BOT_TOKEN="123456789:ABCDefGhIJKlmNoPQRstuVwxYZ"
$ export SNOOZE_E2E_TELEGRAM_CHAT_ID="-100987654321"
$ go test -run TestTelegramE2E ./internal/pluginimpl/telegram/...
```

A message will appear in the configured chat on success.

## Notes & limitations

- **Message length.** The Telegram Bot API enforces a 4096-character limit per message. Very long `Message` records are truncated by the API, not by the plugin; the plugin returns no error in that case but delivery may be partial. Consider shortening the template for high-verbosity sources.
- **Rate limits.** Telegram enforces a [rate limit](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this) of roughly 30 messages per second to a single group and 1 message per second per private chat. Snooze's notification worker does not implement per-chat throttling; heavy alert bursts may cause temporary `429 Too Many Requests` errors (which the plugin surfaces as an API error).
- **MarkdownV2 escaping.** When `parse_mode` is set to `MarkdownV2`, all special characters (`` _ * [ ] ( ) ~ \` > # + - = | { } . ! ``) in dynamic text must be escaped with a backslash. The plugin does not auto-escape for MarkdownV2 — write your template carefully or prefer `HTML` for operator-controlled text.
- **No resolve path.** Telegram messages are one-shot; there is no native "resolve" concept. If you need to indicate a resolution, use a distinct template that checks `{{ if eq .State "close" }}…{{ end }}`.

