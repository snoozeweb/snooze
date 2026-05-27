---
sidebar_position: 26
---

# Google Chat (output)

## Overview

The **Google Chat** notifier is an outbound-only in-process Snooze plugin. When a notification rule matches an alert, the plugin POSTs a message to a Google Chat space via an [Incoming Webhook](https://developers.google.com/workspace/chat/quickstart/webhooks) URL. No external SDK is used — only the Go standard library.

Two message styles are available:

- **Card** (`use_card: true`, the default): a `cardsV2` card with a header (host + severity) and a decorated-text widget containing the rendered message. Cards display more prominently in the Chat UI.
- **Plain text** (`use_card: false`): a simple `{"text": "..."}` message.

Optional reply threading groups related messages under the same thread key (e.g. all notifications for the same alert hash).

:::note

The bidirectional Google Chat bot (`snooze-googlechat` daemon) is a separate, work-in-progress component that subscribes to Pub/Sub and can receive commands from Chat. It is **not** covered by this plugin.

:::

## Configuration

Wire the plugin by creating a **Notification** and adding a **Google Chat** action. The action form accepts the following fields:

| Field | Required | Description |
|----|----|----|
| `webhook_url` | yes | Google Chat Incoming Webhook URL (`https://chat.googleapis.com/v1/spaces/.../messages?key=...&token=...`). |
| `message` | no | Message body as a Go `text/template` over the alert record fields (`.UID`, `.Host`, `.Source`, `.Process`, `.Severity`, `.Message`, `.State`, `.Timestamp`, `.Tags`, `.Hash`). Default: `*{{ .Severity }}* on {{ .Host }}: {{ .Message }}`. |
| `use_card` | no | Boolean switch. When `true` (default), sends a `cardsV2` card. When `false`, sends a plain `{"text":"..."}` message. |
| `thread_key` | no | Go `text/template` rendered to a thread-key string. When non-empty, the request appends `&messageReplyOption=REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD` and includes `{"thread": {"threadKey": "<key>"}}` in the body. Use `{{ .Hash }}` to thread duplicate alerts together. |
| `timeout` | no | Request timeout as a Go duration string (e.g. `10s`, `30s`). Default: `10s`. |

### Field reference

``` yaml
webhook_url: 'https://chat.googleapis.com/v1/spaces/AAAA/messages?key=AIzaSy...&token=xxx'
message: '*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}'
use_card: true
thread_key: '{{ .Hash }}'
timeout: '10s'
```

## End-to-end test setup

1.  In Google Chat, create or open a **Space**.
2.  Click the space name → *Apps & Integrations* → *Add webhooks*.
3.  Give the webhook a name (e.g. `snooze-test`) and copy the generated URL.
4.  Export the URL and run the e2e test:

``` console
$ export SNOOZE_E2E_GOOGLECHAT_WEBHOOK="https://chat.googleapis.com/v1/spaces/.../messages?key=...&token=..."
$ go test -run E2E ./internal/pluginimpl/googlechat/...
```

The test sends two messages (one card, one plain text) to the space and asserts no error is returned.

| Variable | Purpose |
|----|----|
| `SNOOZE_E2E_GOOGLECHAT_WEBHOOK` | Full Incoming Webhook URL for the target test space. |

Environment variables

## Notes & limitations

- **HTTP 200 only**: Google Chat webhooks return HTTP 200 on success. Any other status code is treated as an error and surfaced to the notification worker for retry/dead-letter handling.
- **No authentication header**: Google Chat webhooks carry credentials in the query string (`key` + `token`). The plugin posts the URL as-is.
- **Rate limits**: Google Chat enforces a per-space webhook rate limit (roughly one message per second as of 2026). High-volume deployments should use aggregate rules to reduce notification frequency.
- **cardsV2 formatting**: the card header uses `.Host` as the title and `.Severity` as the subtitle. The message template populates the single `decoratedText` widget. Advanced card layouts (buttons, images, etc.) are not currently supported.
- **Resolve path**: there is no distinct resolve/close action — when `rec.State == "close"` the same template renders with the resolved record fields. Use conditional template logic (`{{ if eq .State "close" }}` ... `{{ end }}`) to customise the message for resolved events.

