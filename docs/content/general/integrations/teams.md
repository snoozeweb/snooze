---
sidebar_position: 28
---

# Microsoft Teams (output / bidirectional)

This integration has two modes:

- **Built-in notifier (easy, recommended):** configured in the Snooze Actions editor — Snooze posts an Adaptive Card directly to a Teams Incoming Webhook URL. No Graph API, no OAuth, no separate process. Start here.
- **Advanced: bidirectional daemon (optional):** the `snooze-teams` daemon adds interactive triage from Teams (`ack`, `close`, `snooze` commands via @-mention). See [below](#advanced-bidirectional-daemon).

## In-process notifier (Incoming Webhook)

The built-in `teams` notifier ships as part of snooze-server. Configure it as a notification
**action** from the web UI (Notifications → Actions → New → *Microsoft Teams*),
or directly as an action document:

```json
{
  "name": "teams-prod",
  "action": {
    "selected": "teams",
    "subcontent": {
      "webhook_url": "https://<org>.webhook.office.com/webhookb2/...",
      "title": "{{ .Severity }} on {{ .Host }}",
      "message": "{{ .Message }}"
    }
  }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `webhook_url` | yes | Teams Incoming Webhook / Workflows URL. |
| `title` | no | Card title; Go `text/template` over the record (default `{{ .Severity }} on {{ .Host }}`). |
| `message` | no | Card body; Go `text/template` over the record (default `{{ .Message }}`). |
| `timeout` | no | Request timeout as a Go duration (default `10s`). |

It posts an **Adaptive Card** — no Graph API, no OAuth, no separate process. The card's accent colour follows severity (Good / Warning / Attention); a resolved alert (`state: close`) renders green with a `✅ Resolved` prefix. Use the **Send test** button in the Actions editor to post a sample card and confirm the webhook URL works end-to-end.

If you need operators to acknowledge or close alerts from within Teams, set up the daemon described below.

## Advanced: bidirectional daemon {#advanced-bidirectional-daemon}

**When to use the daemon instead of (or in addition to) the in-process notifier:**

- You want operators to triage alerts directly from the Teams channel using @-mention commands (`ack`, `close`, `snooze`, `escalate`).
- You want re-escalations to post as threaded replies under the original alert card rather than new messages.

For simple "post a card to a channel" notifications, the in-process notifier is sufficient.

### Overview

**snooze-teams** is a standalone daemon that bridges snooze-server with a Microsoft Teams channel. On the outbound side, it receives alert payloads from snooze-server (via a webhook action POSTing to its `/alert` HTTP endpoint) and renders each alert as an **Adaptive Card 1.4** chatMessage via the Microsoft Graph API. On the inbound side, it polls the same channel for new messages and interprets @mentions directed at the bot as triage commands (`ack`, `close`, `snooze`, etc.) that are forwarded to the Snooze v1 REST API.

The daemon talks to Graph over plain `net/http` using an OAuth2 delegated token obtained once via the `snooze-teams authorize` subcommand. It does **not** use the msgraph-sdk-go or any Microsoft SDK.

### How snooze-server feeds it

Configure a **notification action** of type "webhook" on snooze-server and point it at `http://<daemon-host>:5202/alert`. The webhook plugin POSTs an `alertRequest` JSON body containing a `channels` list (of the form `teams/<teamID>/channels/<channelID>`), the alert record, and optional `reply_to_ids` for thread chaining. The daemon returns an `alertResponse` containing the Graph message ID of the posted card; when the webhook plugin is configured to capture that response the next firing of the same alert will be posted as a reply in the same thread rather than starting a new one.

Both the inbound command channel (Graph polling) and the outbound webhook listener (`/alert`) run concurrently inside a single process. Either subsystem failing tears the daemon down so the supervisor restarts it.

### Alert card format

Each alert is rendered as an **Adaptive Card 1.4** with:

- A bold `⚠️ Received alert ⚠️` header and a subtle timestamp.
- A **FactSet** listing Host, Source, Process, and Severity.
- An emphasis Container with the alert message in bold.
- An `Action.OpenUrl` button labelled "View in Snooze" that links directly to the alert's detail page (requires `server` to be set in the config so the URL can be constructed).

On re-escalation (subsequent firings of the same alert) the daemon posts a succinct HTML reply under the existing thread rather than repeating the full card, following the Teams convention for threaded conversations.

## Configuration

snooze-teams reads `/etc/snooze/teams.yaml` by default (override with `-c`). All fields listed below map directly to YAML keys in that file.

``` yaml
# --- Snooze server connection ---
server: https://snooze.example.com     # Required
username: snooze-bot                   # For the Snooze /login endpoint
password: change-me
method: local                          # local | ldap | anonymous (default: local)
# token: <bearer>                      # Pre-minted bearer token; skips /login

insecure: false                        # Skip TLS verification for Snooze client

# --- Azure AD (Entra ID) application ---
tenant_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx   # Required
client_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx   # Required
# client_secret is only required for auth_mode=client_credentials:
# client_secret: <secret>

# --- OAuth2 flow ---
auth_mode: delegated        # delegated (default) | client_credentials
public_client: true         # Set true for Mobile/Desktop platform app registrations
token_file: /var/lib/snooze/teams-token.json   # Where the refresh token is persisted

# Scopes requested by `snooze-teams authorize` (defaults shown):
# scopes:
#   - offline_access
#   - ChannelMessage.Send
#   - ChannelMessage.Read.All
#   - Team.ReadBasic.All
#   - Channel.ReadBasic.All
#   - Chat.ReadBasic

# --- Target channel ---
team_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx     # Required — Graph team GUID
channel_id: 19:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx@thread.tacv2  # Required

# --- Bot identity ---
bot_name: SnoozeBot           # @mention name the daemon listens for (default: SnoozeBot)

# --- Webhook listener (inbound alerts from snooze-server) ---
listen_addr: "0.0.0.0:5202"  # Empty string disables the listener

# --- Polling (inbound commands from Teams) ---
poll_interval: 10s            # How often to fetch new channel messages (default: 10s)
poll_lookback: 1m             # Initial "since" window on first poll (default: 1m)
request_timeout: 15s          # Per-HTTP-request cap (default: 15s)

# --- Advanced overrides (for tests / private cloud tenants) ---
# graph_base: https://graph.microsoft.com/v1.0
# login_base: https://login.microsoftonline.com
# scope: https://graph.microsoft.com/.default
```

### Field reference

| Key | Meaning |
|----|----|
| `server` | Snooze base URL. **Required** (also needed to build "View in Snooze" links). |
| `username` / `password` | Credentials for the Snooze `/login` endpoint. |
| `method` | Snooze auth backend; defaults to `local`. |
| `token` | Pre-minted Snooze bearer token; skips `/login` when set. |
| `insecure` | Skip TLS verification for the Snooze client. |
| `tenant_id` | Azure AD (Entra ID) tenant GUID or domain. **Required.** |
| `client_id` | Azure AD application (client) ID. **Required.** |
| `client_secret` | App secret; only required when `auth_mode=client_credentials`. |
| `auth_mode` | `delegated` (default) uses a refresh token from `snooze-teams authorize`; `client_credentials` uses the app-only grant (read-only scopes only — cannot post channel messages). |
| `public_client` | Suppresses `client_secret` on the refresh-token grant. Required for app registrations with the "Mobile and desktop applications" platform (the device-code flow for `ChannelMessage.Send` mandates this platform). |
| `token_file` | Path where the OAuth2 refresh token is persisted. Defaults to `/var/lib/snooze/teams-token.json`. |
| `scopes` | OAuth2 scope list requested by `snooze-teams authorize`. Defaults include `offline_access`, `ChannelMessage.Send`, and the read scopes needed for polling. |
| `team_id` | Microsoft Graph team GUID. **Required.** |
| `channel_id` | Graph channel identifier (`19:xxxx@thread.tacv2`). **Required.** |
| `bot_name` | @mention name the polling loop listens for. Defaults to `SnoozeBot`. |
| `listen_addr` | Bind address for the `/alert` webhook receiver (e.g. `0.0.0.0:5202`). Empty disables the listener. |
| `poll_interval` | How often the daemon polls Graph for new messages. Defaults to `10s`. |
| `poll_lookback` | Initial look-back window on the first poll to avoid replaying historical messages. Defaults to `1m`. |
| `request_timeout` | Per-HTTP-request timeout (Graph and Snooze). Defaults to `15s`. |
| `graph_base` | Override the Graph API root. Defaults to `https://graph.microsoft.com/v1.0`. |
| `login_base` | Override the OAuth2 authority. Defaults to `https://login.microsoftonline.com`. |
| `scope` | OAuth2 resource scope for the `client_credentials` grant. Defaults to `https://graph.microsoft.com/.default`. |

### systemd unit

``` ini
[Unit]
Description=Snooze Microsoft Teams notification daemon
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-teams -c /etc/snooze/teams.yaml
Restart=on-failure
RestartSec=5s

# Hardening
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ReadWritePaths=/var/lib/snooze /var/log/snooze

StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

## Setup

### Azure AD application registration

snooze-teams uses the **OAuth2 device-code + delegated** flow for `ChannelMessage.Send` (Microsoft does not expose this permission as an application permission, so the legacy client-credentials path cannot post messages). Follow these steps once per deployment:

1.  In the [Azure portal](https://portal.azure.com) go to **Azure Active Directory → App registrations → New registration**.
2.  Give the app a name (e.g. `snooze-bot`). Choose "Accounts in this organizational directory only".
3.  Under **Authentication**, add a platform: choose **Mobile and desktop applications** and select `https://login.microsoftonline.com/common/oauth2/nativeclient` as the redirect URI.
4.  Still under **Authentication**, set **Allow public client flows** to **Yes**. This is mandatory for the device-code flow.
5.  Under **API permissions**, add the following **Delegated** permissions for Microsoft Graph and grant admin consent:
    - `ChannelMessage.Send`
    - `ChannelMessage.Read.All`
    - `Team.ReadBasic.All`
    - `Channel.ReadBasic.All`
    - `Chat.ReadBasic`
    - `offline_access`
6.  Note the **Application (client) ID** and the **Directory (tenant) ID** — these become `client_id` and `tenant_id` in `teams.yaml`.

### Finding the team and channel IDs

The easiest way is via the Teams web client:

- Open the channel, click the `…` menu next to the channel name, choose **Get link to channel**. The URL contains the `groupId` (team ID) and the `channel` parameter (channel ID, URL-encoded).

Alternatively use the Graph Explorer (`https://developer.microsoft.com/en-us/graph/graph-explorer`) and call `GET /teams` then `GET /teams/{teamId}/channels`.

### Running the device-code authorization flow

Once `teams.yaml` has `tenant_id`, `client_id`, `public_client: true`, and `auth_mode: delegated`, run the one-shot authorization as the `snooze` user (or whichever user the service will run as):

``` console
$ snooze-teams authorize -c /etc/snooze/teams.yaml
```

The command prints a URL and a short user code to stderr, opens a polling loop, and waits for the operator to visit the URL in a browser, sign in with a Teams account that has permission to post in the target channel, and enter the code. On success the resulting refresh token is written to `token_file` (default `/var/lib/snooze/teams-token.json`).

:::note

The refresh token is long-lived but does expire (typically after 90 days of inactivity or on password/MFA changes). Re-run `snooze-teams authorize` whenever the daemon logs a `401` / token-refresh error and restart the service.

:::

## Inbound command handling

After posting an alert card, snooze-teams registers the Teams thread root ID in an in-process cache keyed by `(team, channel, threadID) → alertUID`. When an operator replies in that thread using one of the following commands (or @-mentions the bot in a new message), the daemon looks up the alert UID and acts on it:

| Command | Action |
|----|----|
| `ack [message]` / `acknowledge` / `ok` | Acknowledges the alert. Posts an `ack` comment to Snooze. |
| `close [message]` / `done` | Closes the alert. Posts a `close` comment. |
| `open [message]` / `reopen` / `re-open` | Re-opens a closed alert. Posts an `open` comment. |
| `snooze <duration>` | Creates a Snooze entry for the given duration (e.g. `6h`, `30m`, `2d`, `forever`) and acknowledges the alert. Duration units: `m`/`min`/`mins`, `h`/`hour`/`hours`, `d`/`day`/`days`, `w`/`week`/`weeks`, `y`/`year`/`years`, `month`/`months`. |
| `esc [field=value …] [message]` / `escalate` | Re-escalates the alert, optionally modifying fields. Supported operators: `=` (SET), `+=` (ARRAY_APPEND), `-=` (ARRAY_DELETE). |
| `comment <text>` / any unrecognised text | Adds a free-form comment to the alert without changing its state. |
| `help` | Posts the command list back into the thread. |

Every action is stamped with `method: "teams"` in the Snooze audit trail. The thread cache is in-memory only — a daemon restart clears it. After a restart, existing alert threads will not recognise new replies until the alert is re-fired (which populates the cache again).

## Testing / verifying

### Health check

The webhook listener exposes a `/healthz` endpoint that returns `200 OK` when the daemon is running:

``` console
$ curl -sf http://localhost:5202/healthz && echo ok
```

### Sending a test alert

POST a minimal alert envelope to the `/alert` endpoint (substitute your team and channel IDs):

``` console
$ curl -sS -X POST http://localhost:5202/alert \
    -H 'Content-Type: application/json' \
    -d '{
      "channels": ["teams/<TEAM_ID>/channels/<CHANNEL_ID>"],
      "alert": {
        "host": "web-01",
        "source": "nagios",
        "severity": "critical",
        "message": "disk usage at 95%"
      }
    }'
```

A successful post returns a JSON body with `"delivered"` listing the channel reference and `"message_ids"` mapping the channel to the Graph thread root ID.

## Notes & limitations

- **Delegated auth only for posting.** `ChannelMessage.Send` is a delegated- only permission in Microsoft Graph. `auth_mode: client_credentials` can read messages but cannot post; the daemon will log an authorization error on every outbound alert attempt if configured with the app-only flow.
- **Thread cache is in-memory.** The mapping of Teams thread roots to Snooze alert UIDs is not persisted across daemon restarts. After a restart, replies in pre-existing threads will not be recognised until the alert is re-fired.
- **One level of replies.** Microsoft Graph only supports one level of reply nesting under a channel message. All thread replies (re-escalation updates, command confirmations) land at depth 1 under the original card.
- **Token expiry.** The refresh token stored in `token_file` expires on Microsoft's schedule (90-day inactivity window by default). Run `snooze-teams authorize` and restart the service when this happens.
- **Self-message detection.** The daemon embeds an HTML comment marker (`<!-- snooze-bot -->`) in every message it posts so the polling loop can skip its own output. Removing or filtering that comment will cause a feedback loop.
- **Markdown in FactSet values.** The Adaptive Card specification allows Markdown links inside FactSet `value` fields, but some Teams clients (web, certain mobile builds, tenants with Markdown disabled by policy) strip them silently. The card also includes an `Action.OpenUrl` button as a reliable clickable fallback.
