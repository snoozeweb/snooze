---
sidebar_position: 35
---

# JIRA Cloud (output / bidirectional)

This integration has two modes:

- **Built-in notifier (easy, recommended):** configured entirely in the Snooze Actions editor — Snooze creates a JIRA issue directly, no extra process required. Start here.
- **Advanced: bidirectional daemon (optional):** the `snooze-jira` daemon adds auto-close (JIRA → Snooze) and re-escalation deduplication (comment on an existing ticket instead of opening a duplicate). See [below](#advanced-bidirectional-daemon).

## In-process notifier (recommended)

The built-in `jira` notifier is configured entirely in the Snooze web UI under **Notifications → Actions → New → JIRA**. It calls the JIRA Cloud REST API v3 directly from the Snooze server process — no separate daemon, no extra config file.

**What it does:** for every notification that matches a rule, it creates one new JIRA issue. It is fire-and-forget: there is no deduplication (each notification creates a fresh issue) and no auto-close (resolving the ticket in JIRA does not close the Snooze record). It appears in the Actions gallery under **Ticketing**, alongside ServiceNow.

**Action fields** (configured in the Actions editor):

| Field | Required | Description |
|-------|----------|-------------|
| `jira_url` | yes | JIRA Cloud base URL (e.g. `https://mycompany.atlassian.net`). |
| `jira_email` | yes | Atlassian account email for HTTP Basic auth. |
| `jira_api_token` | yes | Atlassian Cloud API token paired with `jira_email`. |
| `project_key` | yes | JIRA project key (e.g. `OPS`). |
| `issue_type` | no | Issue type name (default: `Task`). |
| `priority` | no | Issue priority (default: `Medium`). |
| `summary_template` | no | Go `text/template` for the issue summary (default: `[{{ .Severity }}] {{ .Host }} - {{ .Message }}`). |
| `description_template` | no | Go `text/template` for the issue description. |
| `labels` | no | Labels to apply to every new issue (default: `["snooze"]`). |
| `timeout` | no | Per-request timeout (default: `30s`). |

Use the **Send test** button in the Actions editor to create a sample issue and confirm the connection works end-to-end.

If you need deduplication, auto-close, or re-escalation comments on an existing ticket, use the daemon described below.

## Advanced: bidirectional daemon {#advanced-bidirectional-daemon}

**When to use the daemon instead of (or in addition to) the in-process notifier:**

- You want resolving a JIRA ticket to automatically close the corresponding Snooze record (bidirectional poller).
- You want re-escalations to add a comment to the existing ticket rather than opening a duplicate.
- You want finer control over issue transitions (`initial_status`, `reopen_closed`).

For simple ticket creation without any of the above, the in-process notifier is sufficient.

### Overview

**snooze-jira** is a standalone daemon that bridges snooze-server with JIRA Cloud. It exposes a small HTTP server that snooze-server hits as a **webhook action** to create and update JIRA issues from Snooze alerts. An optional background poller closes Snooze records when the corresponding JIRA ticket transitions to a Done status category.

On the first alert for a given record the daemon creates a new JIRA issue. On re-escalations it adds a comment to the existing ticket and, when `reopen_closed` is enabled, transitions a Done ticket back to an open workflow status. The daemon uses the **JIRA REST API v3** with HTTP Basic auth (Atlassian Cloud API token); issue descriptions and comments are formatted in **ADF** (Atlassian Document Format).

### How snooze-server feeds it

Configure a **notification action** of type "webhook" on snooze-server and point it at `http://<daemon-host>:5203/alert`. The webhook plugin POSTs one or more alert envelopes (either a single JSON object or a JSON array). Each envelope may carry a `project_key` override; the daemon falls back to the `project_key` configured in `jira.yaml`.

The webhook endpoint also accepts a `snooze_action_name` query parameter that is recorded in log output for correlation.

### Optional bidirectional poller

When `alert_hash_custom_field` is set, the daemon enables a background poller that periodically queries JIRA for tickets in a non-Done status category, checks whether their linked Snooze record is still open, and closes the Snooze record when the JIRA ticket has moved to Done. This is the "JIRA → Snooze" direction: resolving a ticket in JIRA automatically closes the alert in Snooze.

The Snooze client credentials (`server`, `username`/`password` or `token`) are only required when the poller is enabled.

## Configuration

snooze-jira reads `/etc/snooze/jira.yaml` by default (override with `-c`).

``` yaml
# --- JIRA Cloud connection ---
jira_url: https://mycompany.atlassian.net   # Required
jira_email: bot@example.com                 # Required — Atlassian account email
jira_api_token: ATATxxxxxxxxxxxxxxxxxxxx     # Required — Atlassian API token
ssl_verify: true                             # Set false for self-signed JIRA proxies

# --- Default project / issue settings ---
project_key: OPS                # Required — default JIRA project key
issue_type: Task                # Default issue type (default: Task)
# issue_type_id: "10001"        # Override issue_type with a numeric type ID
priority: Medium                # Fallback priority when severity is not in priority_mapping
labels:                         # Labels applied to new issues (default: [snooze])
  - snooze
summary_template: "[${severity}] ${host} - ${message}"   # Default shown
# description_template: |       # Overrides ADF auto-description; supports
#   Host: ${host}               # ${severity}, ${host}, ${source}, ${process},
#   Severity: ${severity}       # ${message}, ${timestamp}, ${hash}, ${snooze_url}
#   ${message}
assignee: ""                    # JIRA accountId or email; empty = unassigned
reporter: ""                    # JIRA accountId or email; empty = project default
extra_fields: {}                # Additional JIRA fields for every new issue
custom_fields: {}               # customfield_XXXXX → value for every new issue

# --- Priority mapping (Snooze severity → JIRA priority name) ---
# Defaults shown below; override any entry:
# priority_mapping:
#   emergency: Critical
#   critical:  High
#   warning:   Medium
#   minor:     Low
#   info:      Lowest

# --- Re-open behaviour ---
reopen_closed: false            # Reopen Done tickets on re-escalation (default: false)
reopen_status_name: "To Do"     # Workflow status to transition back to (default: To Do)
initial_status: ""              # Transition a new issue to this status immediately

# --- Snooze ↔ JIRA link (required for the poller) ---
alert_hash_custom_field: ""     # JIRA custom field id, e.g. "customfield_10500"
                                # Stores the Snooze record URL; enables the poller.

# --- Bidirectional poller ---
poll_enabled: true              # Silently disabled when alert_hash_custom_field is empty
poll_interval: 5m               # How often to query JIRA (default: 5m)
poll_jql: ""                    # Override the auto-derived JQL query
poll_max_results: 100           # Per-cycle result cap (default: 100)

# --- Snooze client (for the poller) ---
server: https://snooze.example.com   # Required when poll_enabled and alert_hash_custom_field set
username: snooze-bot
password: change-me
method: local                        # local | ldap | anonymous (default: local)
# token: <bearer>                    # Pre-minted bearer token; skips /login
insecure: false

# --- Webhook listener ---
listening_address: "0.0.0.0"    # Bind address (default: 0.0.0.0)
listening_port: 5203            # Bind port (default: 5203)
snooze_url: http://localhost:5200   # Snooze Web UI origin for links in JIRA descriptions
message_limit: 10               # Max alerts processed per webhook call (default: 10)
request_timeout: 30s            # Per-JIRA-request timeout (default: 30s)

debug: false
```

### Field reference

| Key | Meaning |
|----|----|
| `jira_url` | JIRA Cloud base URL. **Required.** |
| `jira_email` | Atlassian account email for HTTP Basic auth. **Required.** |
| `jira_api_token` | Atlassian Cloud API token paired with `jira_email`. **Required.** |
| `ssl_verify` | When `false`, TLS verification for the JIRA client is skipped. Defaults to `true`. |
| `project_key` | Default JIRA project key (e.g. `OPS`). **Required.** Can be overridden per-payload. |
| `issue_type` | Default issue type name (e.g. `Task`, `Bug`). Defaults to `Task`. |
| `issue_type_id` | JIRA issue type ID; overrides `issue_type` when set. |
| `priority` | Fallback priority when the alert's severity is not in `priority_mapping`. Defaults to `Medium`. |
| `priority_mapping` | Map of Snooze severity → JIRA priority name. Defaults: `emergency→Critical`, `critical→High`, `warning→Medium`, `minor→Low`, `info→Lowest`. |
| `labels` | Labels applied to every new issue. Defaults to `["snooze"]`. |
| `summary_template` | Go-style template for the issue summary. Variables: `${severity}`, `${host}`, `${source}`, `${process}`, `${message}`, `${timestamp}`. Defaults to `[${severity}] ${host} - ${message}`. |
| `description_template` | Overrides the auto-generated ADF description. Supports the `summary_template` variables plus `${hash}` and `${snooze_url}`. Each line becomes an ADF paragraph. |
| `assignee` | Default assignee — Atlassian `accountId` or email (resolved via `/user/search`). |
| `reporter` | Default reporter. Same resolution as `assignee`. |
| `extra_fields` | Additional JIRA fields applied to every new issue (e.g. `components`, `fixVersions`). |
| `custom_fields` | `customfield_XXXXX → value` map applied to every new issue. Values are passed through to the JIRA API as-is. |
| `reopen_closed` | When `true`, a Done ticket is transitioned back to `reopen_status_name` on re-escalation. Defaults to `false`. |
| `reopen_status_name` | Workflow status to transition a Done ticket back to. Defaults to `To Do`. |
| `initial_status` | When set, a freshly created issue is immediately transitioned to this workflow status. |
| `alert_hash_custom_field` | JIRA custom field ID (e.g. `customfield_10500`) used to store the Snooze record URL. **Required to enable the poller** — without it there is no way to correlate a JIRA ticket back to a Snooze record. |
| `poll_enabled` | Enables the background poller. Defaults to `true`; silently disabled when `alert_hash_custom_field` is empty. |
| `poll_interval` | How often the poller queries JIRA. Defaults to `5m`. |
| `poll_jql` | Overrides the auto-derived JQL query. When empty the daemon derives: `cf[XXXXX] is not EMPTY AND statusCategory != Done`. |
| `poll_max_results` | Maximum results returned per poller cycle. Defaults to `100`. |
| `server` | Snooze base URL for the poller's Snooze client. Required when the poller is active. |
| `username` / `password` | Snooze credentials for the `/login` endpoint. |
| `method` | Snooze auth backend; defaults to `local`. |
| `token` | Pre-minted Snooze bearer token; skips `/login`. |
| `insecure` | Skip TLS verification for the Snooze client. |
| `listening_address` | Bind address for the `/alert` webhook receiver. Defaults to `0.0.0.0`. |
| `listening_port` | Bind port for the webhook receiver. Defaults to `5203`. |
| `snooze_url` | Snooze Web UI origin used to build links inside JIRA descriptions and the `alert_hash_custom_field` value. Defaults to `http://localhost:5200`. |
| `message_limit` | Maximum alerts processed per single webhook POST. Defaults to `10`. |
| `request_timeout` | Per-request timeout for JIRA API calls. Defaults to `30s`. |
| `debug` | Enables debug-level logging. |

### systemd unit

``` ini
[Unit]
Description=Snooze JIRA notification daemon
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-jira -c /etc/snooze/jira.yaml
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

### Atlassian API token

1.  Sign in to your Atlassian account at <https://id.atlassian.com/manage-profile/security/api-tokens>.
2.  Click **Create API token**, give it a label (e.g. `snooze-jira`), and copy the generated token.
3.  Set `jira_email` to the email address of the Atlassian account and `jira_api_token` to the copied token in `jira.yaml`.

The account must have **Create Issue** and **Add Comment** permissions in the target project. If you want the daemon to transition tickets (`initial_status` or `reopen_closed`), it also needs the **Transition Issues** project permission.

### Project key

The project key is the short prefix shown before each issue number (e.g. the `OPS` in `OPS-123`). It can be found on the JIRA project settings page or in the URL. Set `project_key` in `jira.yaml`.

### Custom field for the poller

To enable the bidirectional poller, create a custom field in JIRA that will store the Snooze record URL:

1.  Go to **JIRA Settings → Issues → Custom fields** and create a new "URL" (or "Text") field, e.g. named `Snooze URL`.
2.  Note the field's ID (visible in the URL when editing it, or in the field list as `customfield_XXXXX`).
3.  Set `alert_hash_custom_field: customfield_XXXXX` in `jira.yaml`.
4.  Add the field to the project's issue screens so it is visible and searchable.

### snooze-server webhook action

In the snooze-server web UI, create a **notification action** of type "webhook" with:

- URL: `http://<daemon-host>:5203/alert`
- Method: `POST`
- Content-Type: `application/json`

The action can be triggered by a **notification** rule scoped to the alerts you want tracked in JIRA.

## Testing / verifying

### Health check

``` console
$ curl -sf http://localhost:5203/healthz && echo ok
```

### Sending a test alert

``` console
$ curl -sS -X POST http://localhost:5203/alert \
    -H 'Content-Type: application/json' \
    -d '{
      "project_key": "OPS",
      "alert": {
        "host": "db-01",
        "source": "prometheus",
        "severity": "critical",
        "message": "PostgreSQL replication lag > 60s"
      }
    }'
```

The daemon responds with `200 OK` and a JSON body summarising the created (or updated) JIRA issue reference. Check the JIRA project board to confirm the issue appeared.

## Notes & limitations

- **JIRA Cloud only.** The daemon targets the JIRA Cloud REST API v3 (`/rest/api/3`). JIRA Server / Data Center uses a different authentication model (session cookies or Personal Access Tokens) and may need adjustments.
- **ADF descriptions.** Issue descriptions are formatted in Atlassian Document Format. The `description_template` config key allows plain-text lines that are each wrapped in an ADF paragraph node; it cannot currently express rich ADF inline formatting.
- **Poller requires the custom field.** The background poller is silently disabled when `alert_hash_custom_field` is empty. Without the field there is no way to map a JIRA ticket back to its Snooze record.
- **Transition availability.** `reopen_status_name` and `initial_status` must name a workflow status that is reachable from the ticket's current status via an existing transition. The daemon logs a warning if no matching transition is found.
- **Assignee / reporter resolution.** When `assignee` or `reporter` is set to an email address, the daemon calls `GET /rest/api/3/user/search` to resolve it to an Atlassian `accountId`. A failed lookup is logged and the field is omitted from the issue creation payload rather than aborting.
- **Message limit.** A single webhook POST is capped at `message_limit` alerts (default 10). Batches larger than this are truncated with a warning; split large notification actions or increase the limit if needed.
