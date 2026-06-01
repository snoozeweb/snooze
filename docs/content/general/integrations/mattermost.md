---
sidebar_position: 27
---

# Mattermost (output / bidirectional)

## Overview

**snooze-mattermost** is a standalone, bidirectional daemon that connects Snooze to a Mattermost server. It operates in two directions simultaneously:

- **Outbound (Snooze → Mattermost):** snooze-server sends alert notifications to the daemon via a webhook action POSTing to its `/alert` endpoint; the daemon posts each alert as a Mattermost message in the configured channel.
- **Inbound (Mattermost → Snooze):** the daemon connects to the Mattermost server over a persistent **WebSocket** and listens for `posted` events. Messages that @-mention the bot or begin with `/snooze` are parsed as triage commands and forwarded to the Snooze v1 REST API. The bot replies inline in the same thread.

The daemon does **not** use the official Mattermost server SDK (which would pull in hundreds of megabytes of transitive dependencies). All wire shapes are hand-rolled against the Mattermost v4 REST and WebSocket APIs.

### How snooze-server feeds it

Configure a **notification action** of type "webhook" on snooze-server and point it at `http://<daemon-host>/alert` (the default port is set by your `listen_addr` in `mattermost.yaml` — this is not currently a standard config key; the webhook receiver is an outbound-only path if you are running the daemon purely for inbound commands). The primary integration path for alert delivery is to have the webhook plugin POST to the daemon, which then calls the Mattermost REST API to post the message.

The WebSocket connection is managed independently: on startup the daemon resolves the bot's identity and team membership, then opens a WebSocket to `wss://<mattermost_url>/api/v4/websocket`. On disconnect it sleeps with exponential back-off (starting at `reconnect_initial_backoff`, doubling each attempt up to `reconnect_max_backoff`) and reconnects automatically.

## Configuration

snooze-mattermost reads `/etc/snooze/mattermost.yaml` by default. Override the path with the `-c` flag or the `SNOOZE_MATTERMOST_CONFIG` environment variable.

``` yaml
# --- Snooze server connection ---
server: https://snooze.example.com   # Required
username: snooze-bot                 # For the Snooze /login endpoint
password: change-me
method: local                        # local | ldap | anonymous (default: local)
insecure: false                      # Skip TLS verification for the Snooze client

# --- Mattermost connection ---
mattermost_url: https://mm.example.com     # Required — Mattermost site origin
mattermost_token: xxxxxxxxxxxxxxxxxxxx     # Required — personal access token
mattermost_team: my-team                   # Required — team name (slug)

# --- Channel scope ---
channels:                       # Optional; empty means all channels the bot can see
  - alerts
  - ops

# --- Bot identity ---
bot_name: snooze                 # @mention name (default: snooze)

# --- WebSocket keepalive ---
ping_interval: 30s               # WS ping cadence (default: 30s)

# --- Reconnect back-off ---
reconnect_initial_backoff: 1s    # First delay before reconnect (default: 1s)
reconnect_max_backoff: 1m        # Back-off cap (default: 1m)
```

### Field reference

| Key | Meaning |
|----|----|
| `server` | Snooze base URL. **Required.** |
| `username` / `password` | Credentials for the Snooze `/login` endpoint. |
| `method` | Snooze auth backend; defaults to `local`. |
| `insecure` | Skip TLS verification for the Snooze client. |
| `mattermost_url` | Mattermost site origin (e.g. `https://mm.example.com`). **Required.** |
| `mattermost_token` | Personal access token used as the bearer for all REST calls and the WebSocket authentication frame. **Required.** |
| `mattermost_team` | Team name (slug — the short name in the URL, not the display name). **Required.** |
| `channels` | List of channel names the bot should respond in. Empty means the bot responds in any channel it has been added to. |
| `bot_name` | The @mention name the daemon listens for. Defaults to `snooze`. |
| `ping_interval` | WebSocket keepalive cadence. Defaults to `30s`. |
| `reconnect_initial_backoff` | Delay before the first reconnect attempt after a WebSocket disconnect. Doubles each attempt. Defaults to `1s`. |
| `reconnect_max_backoff` | Upper bound for the reconnect back-off. Defaults to `1m`. |

### systemd unit

``` ini
[Unit]
Description=Snooze Mattermost notification daemon
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-mattermost -c /etc/snooze/mattermost.yaml
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

### Creating a bot account and personal access token

1.  In Mattermost go to **System Console → Integrations → Bot Accounts** and enable bot accounts if not already on.
2.  Create a bot account (e.g. `snooze`) via **Integrations → Bot Accounts → Add Bot Account**. Assign it the **Member** role in the target team and add it to the channels you want it to monitor.
3.  Copy the access token generated on creation (you will not see it again). If you lose it, deactivate and recreate the bot or reset the token via the API.
4.  Alternatively, use a **personal access token** of an existing Mattermost user account: go to **Account Settings → Security → Personal Access Tokens**.
5.  Set `mattermost_token` to the copied token in `mattermost.yaml`.

:::note

Personal access tokens must be enabled by the Mattermost system admin (**System Console → Integrations → Integration Management → Enable Personal Access Tokens**).

:::

### Team and channel names

`mattermost_team` takes the team's URL slug — the short name visible in the Mattermost URL (`https://mm.example.com/<team-slug>/...`), not the display name. `channels` takes channel slugs in the same format.

Invite the bot to each channel it should monitor. If `channels` is left empty the daemon responds in every channel the bot account has been added to — restricting to a specific list is recommended in production.

### snooze-server webhook action (outbound alerts)

In the snooze-server web UI, create a **notification action** of type "webhook" targeting the daemon's `/alert` endpoint so Snooze pushes alerts to Mattermost.

## Inbound command handling

The daemon reads every `posted` WebSocket event in the configured channels and identifies a message as a bot invocation when it:

- starts with `@<bot_name>` (e.g. `@snooze`), or
- starts with `/snooze`.

The leading prefix is stripped and the remainder is parsed as a command. The bot ignores its own messages to prevent feedback loops.

### Command reference

| Command | Action |
|----|----|
| `/snooze ack <uid> [message]` `acknowledge` / `ok` | Acknowledges the alert identified by `<uid>`. Posts an `ack` comment to Snooze. |
| `/snooze close <uid> [message]` `done` | Closes the alert. Posts a `close` comment. |
| `/snooze reopen <uid> [message]` `open` / `re-open` | Re-opens a closed alert. Posts an `open` comment. |
| `/snooze comment <uid> <message>` | Adds a free-form comment to the alert without changing its state. |
| `/snooze help` | Posts the command list back into the channel. |

All commands require the alert UID (the `uid` field of the Snooze record) as the first argument after the verb. Every action is stamped with `method: "mattermost"` and the Mattermost display name of the requester in the Snooze audit trail.

### Example interaction

``` text
# In Mattermost channel #alerts
@snooze ack abc123def  investigating now
→ ✅ Alert `abc123def` acknowledged by `alice`.

@snooze close abc123def  issue resolved after patching
→ ✅ Alert `abc123def` closed by `alice`.

@snooze help
→ **Snooze bot** — available commands:
  `/snooze ack <uid> [msg]` — acknowledge an alert
  ...
```

The bot replies in the same thread as the triggering message.

### Finding the alert UID

The alert UID is displayed in the Snooze web UI on the alert detail page and is included in the alert payload delivered by notification actions. If your alert notification template includes the UID (e.g. in the message body or via a template variable), operators can copy it directly from the Mattermost notification post.

## Testing / verifying

### Verify the token and team resolution

Start the daemon manually and watch stderr:

``` console
$ snooze-mattermost -c /etc/snooze/mattermost.yaml -debug
```

On a successful startup you should see a line like:

``` text
mattermost handshake ok   user=snooze team=my-team channels=2
```

followed by:

``` text
mattermost ws connected   url=https://mm.example.com
```

If the token is invalid the daemon exits immediately with a "validate token" error. If the team name is wrong it exits with a "resolve team" error.

### Sending a test command from Mattermost

In the configured channel, type:

``` text
@snooze help
```

The bot should reply with the command list within a few seconds. If there is no reply, check:

1.  The bot account has been invited to the channel.
2.  The channel name is listed under `channels` (or `channels` is empty).
3.  The daemon's stderr log for WebSocket or Snooze API errors.

## Notes & limitations

- **WebSocket transport only.** The daemon relies entirely on the Mattermost WebSocket event stream for inbound command delivery. It does not set up an HTTP slash-command endpoint — the `/snooze …` syntax is parsed from regular messages, not from the Mattermost slash-command registration mechanism.
- **UID-based commands.** Every action verb requires an explicit alert UID. There is no thread-based correlation (unlike snooze-teams): the bot does not infer which alert a reply belongs to from the channel thread. Operators must copy the UID from the notification post or the Snooze web UI.
- **Channel allow-list.** When `channels` is empty the daemon accepts commands from any channel the bot account can see. In shared or public Mattermost instances it is strongly recommended to set an explicit list.
- **No \`\`snooze\`\` verb.** The Mattermost daemon supports `ack`, `close`, `reopen`, and `comment` — but not a timed `snooze <duration>` command (unlike snooze-teams). Use the Snooze web UI or the MCP tool to create a snooze entry.
- **Reconnect back-off.** On WebSocket disconnect the daemon waits `reconnect_initial_backoff` before the first retry, doubling each attempt up to `reconnect_max_backoff`. During a reconnect window inbound commands are not processed; outbound alert delivery (if the webhook path is also used) is unaffected as it goes through the REST API directly.
- **No SDK dependency.** The daemon implements only the WebSocket message shapes it needs (`posted` events) hand-rolled against the Mattermost v4 wire format. Some Mattermost versions stringify the `post` field inside the `posted` event data; the daemon handles both the stringified and inline- object forms for robustness.

## In-process notifier (Incoming Webhook)

Besides the standalone `snooze-mattermost` daemon, snooze-server ships a
lightweight **in-process `mattermost` notifier** for the simple "push a message
to a channel" case (no separate process, no bidirectional command handling).

It posts to a Mattermost **Incoming Webhook** URL using the Slack-compatible
attachment payload. Configure it as a notification **action** from the web UI
(Notifications → Actions → New → *Mattermost*), or directly:

```json
{
  "name": "mattermost-prod",
  "action": {
    "selected": "mattermost",
    "subcontent": {
      "webhook_url": "https://mattermost.example.com/hooks/xxxxxxxx",
      "channel": "alerts",
      "message": "*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}"
    }
  }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `webhook_url` | yes | Mattermost Incoming Webhook URL. |
| `channel` | no | Channel override (defaults to the webhook's configured channel). |
| `username` | no | Display-name override for the posting bot. |
| `icon_url` | no | Avatar URL for the posting bot. |
| `message` | no | Go `text/template` over the record (default ``*{{ .Severity }}* on `{{ .Host }}`: {{ .Message }}``). |
| `timeout` | no | Request timeout as a Go duration (default `10s`). |

The attachment colour follows severity; a resolved alert renders green with a
`✅ Resolved` prefix. Use the **Send test** button in the Actions editor to verify
delivery.
