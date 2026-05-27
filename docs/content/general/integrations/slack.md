---
sidebar_position: 23
---

# Slack (output)

## Overview

The **slack** plugin is an in-process Notifier that posts alert notifications to a Slack channel as a [Block Kit](https://api.slack.com/block-kit) message with a severity-coloured attachment sidebar.

Two delivery modes are supported:

- **Incoming Webhook** (default): set `webhook_url` to a Slack Incoming Webhook URL (`https://hooks.slack.com/services/...`). No bot setup required. Slack returns `HTTP 200` with the literal body `ok` on success.
- **Bot Token**: set `bot_token` to a Slack Bot OAuth token (`xoxb-…`) and `channel` to the target channel ID or name. The plugin calls `https://slack.com/api/chat.postMessage` with an `Authorization: Bearer <token>` header. Slack always returns `HTTP 200` and signals logical failures via `{"ok":false,"error":"…"}`; the plugin decodes that response and surfaces errors accordingly.

The plugin uses `net/http` only — no Slack SDK or external library is required.

## Configuration

Wire the plugin through a **Notification → Action** in the Snooze UI or configuration file. Set the action type to `slack` and fill the `action_form` fields described below.

### Field reference

| Field | Default | Description |
|----|----|----|
| `webhook_url` | *(see note)* | Slack Incoming Webhook URL (`https://hooks.slack.com/services/T.../B.../...`). Required unless `bot_token` is set. |
| `bot_token` | *(optional)* | Slack Bot OAuth token (`xoxb-…`). When set the plugin uses `chat.postMessage` instead of the Incoming Webhook. At least one of `webhook_url` or `bot_token` must be provided. |
| `channel` | *(optional)* | Channel ID or name (e.g. `C01234ABCDE` or `#alerts`). Required when using `bot_token` mode. |
| `message` | `` *{{ .Severity }}* on \`{{ .Host }}\`: {{ .Message }} `` | Go `text/template` rendered over the alert record. Available fields: `.UID`, `.Host`, `.Source`, `.Process`, `.Severity`, `.Message`, `.State`, `.Timestamp`, `.Tags`. |
| `username` | *(bot name)* | Display-name override for the bot. Incoming Webhook mode only. |
| `icon_emoji` | *(bot icon)* | Emoji to use as the bot icon, e.g. `:robot_face:`. Incoming Webhook mode only. |
| `timeout` | `10s` | HTTP request timeout as a Go duration string (e.g. `5s`, `30s`). |

``` yaml
webhook_url: "https://hooks.slack.com/services/T00000000/B00000000/xxxxxxxxxxxx"
message: "*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}"
username: "Snooze Alerts"
icon_emoji: ":bell:"
timeout: "10s"
```

``` yaml
bot_token: "xoxb-111111111111-222222222222-xxxxxxxxxxxxxxxxxxxx"
channel: "C01234ABCDE"
message: "*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}"
timeout: "10s"
```

### Severity colour mapping

The Block Kit attachment colour bar is derived from the record's `severity` field:

| Severity | Slack colour | Hex (approximate) |
|----|----|----|
| `info`, `notice`, `debug` | `good` | `#36a64f` |
| `warning` | `warning` | `#daa038` |
| `error`, `critical`, `emergency`, *(unknown)* | `danger` | `#d00000` |
| `close` *(resolved)* | `good` | `#36a64f` |

When the record's `state` field is `"close"` (a resolution event), the colour is always `good` regardless of severity, and the rendered message is prefixed with `✅ Resolved:`.

## End-to-end test setup

To run the end-to-end test you need a Slack channel with either an Incoming Webhook or a bot token configured.

**Incoming Webhook setup:**

1.  Go to `https://api.slack.com/apps` → create or select your app.
2.  Under **Incoming Webhooks**, activate the feature and click **Add New Webhook to Workspace**.
3.  Select a channel and copy the webhook URL.

**Bot Token setup:**

1.  Go to `https://api.slack.com/apps` → select your app.
2.  Under **OAuth & Permissions**, add the `chat:write` scope.
3.  Install the app to your workspace and copy the **Bot User OAuth Token** (`xoxb-…`).
4.  Invite the bot to the target channel (`/invite @your-bot`).

**Running the test:**

``` console
# Incoming Webhook mode (minimum):
$ export SNOOZE_E2E_SLACK_WEBHOOK="https://hooks.slack.com/services/T.../B.../..."
$ go test -v -run TestSlackE2E ./internal/pluginimpl/slack/...

# Bot Token mode (optional, both variables required):
$ export SNOOZE_E2E_SLACK_BOT_TOKEN="xoxb-..."
$ export SNOOZE_E2E_SLACK_CHANNEL="#alerts"
$ go test -v -run TestSlackE2E ./internal/pluginimpl/slack/...
```

The test sends one or two real messages to the configured channel and asserts no error is returned. Inspect the channel to verify the message appearance.

**Environment variables read by the e2e test:**

| Variable | Purpose |
|----|----|
| `SNOOZE_E2E_SLACK_WEBHOOK` | Slack Incoming Webhook URL. The test is skipped when both this and `SNOOZE_E2E_SLACK_BOT_TOKEN` are unset. |
| `SNOOZE_E2E_SLACK_BOT_TOKEN` | Slack Bot OAuth token. When set together with `SNOOZE_E2E_SLACK_CHANNEL`, the test additionally exercises bot-token mode. |
| `SNOOZE_E2E_SLACK_CHANNEL` | Target channel ID or name for bot-token mode (e.g. `C01234ABCDE` or `#alerts`). |

## Notes & limitations

- Only the Block Kit `section` block type is used for the message body. Richer layouts (e.g. dividers, context blocks, interactive components) are not supported.
- In bot-token mode the `username` and `icon_emoji` fields are ignored; those cosmetic overrides require the `chat:write.customize` scope and are only supported for Incoming Webhooks.
- Slack rate-limits Incoming Webhooks to 1 request per second (`HTTP 429`). The plugin does not implement client-side rate limiting or automatic retries; the notification worker is responsible for retry and dead-letter handling.
- Slack Web API (bot-token mode) enforces per-method rate limits. Consult [Slack API rate limits](https://api.slack.com/docs/rate-limits) for current tier information.
- The `timeout` field controls the full HTTP round-trip including TLS handshake. The default `10s` is suitable for most deployments.
- HTTPS is required by both Slack endpoints; HTTP webhook URLs will be rejected by Slack.

