# JIRA Output Plugin

This plugin creates JIRA tickets from SnoozeWeb alerts. When an alert is triggered, the plugin creates a new JIRA issue with the alert details. On re-escalation, it adds a comment to the existing ticket instead of creating a duplicate.

## Installation

```console
$ sudo /opt/snooze/bin/pip install git+https://github.com/snoozeweb/snooze_plugins.git#subdirectory=output/jira
$ sudo tee <<SERVICE /etc/systemd/system/snooze-jira.service
[Unit]
Description=Snooze JIRA output plugin
After=network.service

[Service]
User=snooze
ExecStart=/opt/snooze/bin/snooze-jira
Restart=always

[Install]
WantedBy=multi-user.target
SERVICE

$ sudo systemctl daemon-reload
$ sudo systemctl enable snooze-jira
$ sudo systemctl start snooze-jira
```

## Prerequisites

* [Snooze Client](https://github.com/snoozeweb/snooze_client): For the Snooze JIRA daemon to use Snooze Server API
* [JIRA Cloud API Token](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/): Create an API token in your Atlassian account settings
* Snooze Action (webhook): Communication between Snooze Server and the Snooze JIRA daemon. See below

## Create Action

In SnoozeWeb, go to the _Actions_ tab then click on **New**

Configuration hints:
* In _Action_, select `Call a webhook`
* In _URL_, put the alert endpoint of the plugin's daemon (if the daemon runs on the same server as Snooze-server: `http://localhost:5203/alert`)
* In _Payload_, put:
  ```json
  {"project_key": "OPS", "alert": {{ __self__ | tojson() }}}
  ```
  * Replace `OPS` with your JIRA project key
  * You can optionally add `"message": "Custom message"` to include extra context
  * You can override per-alert: `"issue_type": "Bug"`, `"issue_type_id": "10001"`, `"priority": "High"`, `"labels": ["critical", "snooze"]`, `"assignee": "<account_id_or_email>"`, `"reporter": "<account_id_or_email>"`, `"initial_status": "In Progress"`, `"custom_fields": {"customfield_10100": {"value": "Networking"}}`
* Check `Inject Response`
* Check `Batch` if you want multiple alerts to create separate tickets

### Full payload example

```json
{
  "project_key": "OPS",
  "issue_type": "Bug",
  "priority": "High",
  "labels": ["snooze", "production"],
  "alert": {{ __self__ | tojson() }},
  "message": "Requires immediate attention"
}
```

## Create Notification

In SnoozeWeb, go to the _Notifications_ tab then click on **New** or **Edit** an existing notification.
In _Actions_, select the one you just created.

## Configuration

This plugin's configuration is in the following YAML file: `/etc/snooze/jira.yaml` (`/etc/snooze` can be overridden by the environment variable `SNOOZE_JIRA_PATH`)

| Parameter | Type | Default | Description |
|---|---|---|---|
| `jira_url` | String | **required** | JIRA Cloud base URL (e.g. `https://mycompany.atlassian.net`) |
| `jira_email` | String | **required** | Email address for JIRA API authentication |
| `jira_api_token` | String | **required** | JIRA API token (create at [Atlassian API tokens](https://id.atlassian.com/manage-profile/security/api-tokens)) |
| `project_key` | String | **required** | Default JIRA project key (e.g. `OPS`) |
| `issue_type` | String | `Task` | Default issue type (e.g. `Task`, `Bug`, `Story`) |
| `issue_type_id` | String/Integer | | Default Jira issue type ID (e.g. `10001`). When set, it overrides `issue_type` |
| `priority` | String | `Medium` | Default issue priority, used when severity is not found in `priority_mapping` |
| `priority_mapping` | Dict | see below | Maps Snooze alert severity to JIRA priority name |
| `labels` | List | `["snooze"]` | Default labels to add to created tickets |
| `summary_template` | String | `[${severity}] ${host} - ${message}` | Template for issue summary. Available variables: `${severity}`, `${host}`, `${source}`, `${process}`, `${message}`, `${timestamp}` |
| `description_template` | String | | Template for issue description body. When set, replaces the default rich description. Available variables: `${severity}`, `${host}`, `${source}`, `${process}`, `${message}`, `${timestamp}`, `${hash}`, `${snooze_url}`. Supports multi-line (each line becomes a paragraph). If not set, uses a rich default with bold labels and a Snooze link |
| `extra_fields` | Dict | `{}` | Additional JIRA fields to set on issue creation (e.g. `{"components": [{"name": "Infrastructure"}]}`) |
| `assignee` | String | | Default assignee — JIRA account ID (e.g. `5b109f2e9729b51b54dc274d`) or email address (e.g. `user@example.com`). Can be overridden per-alert in payload |
| `reporter` | String | | Default reporter — JIRA account ID or email address. Can be overridden per-alert in payload |
| `custom_fields` | Dict | `{}` | Arbitrary JIRA custom fields to set on issue creation. Values are passed through as-is to the JIRA API. See examples below |
| `reopen_closed` | Boolean | `false` | When true, re-escalation on a closed/done JIRA ticket will reopen it |
| `reopen_status_name` | String | `To Do` | Target status name when reopening a closed ticket (e.g. `To Do`, `Open`, `Backlog`) |
| `initial_status` | String | | If set, newly created issues are transitioned to this status after creation (e.g. `In Progress`, `Open`). Can be overridden per-alert in payload |
| `alert_hash_custom_field` | String | | JIRA custom field ID (e.g. `customfield_10500`) to store the Snooze alert URL. This enables the poller to track tickets and sync status back to Snooze. The field should be a simple text field |
| `poll_enabled` | Boolean | `true` | Enable background polling to close Snooze alerts when JIRA tickets are resolved. Requires `alert_hash_custom_field` to be set (automatically disabled if not configured) |
| `poll_interval` | Integer | `300` | Seconds between poll cycles |
| `poll_jql` | String | auto-generated | Custom JQL query for the poller. If not set, defaults to `cf[XXXXX] is not EMPTY AND statusCategory != Done` based on `alert_hash_custom_field` |
| `ssl_verify` | Boolean | `true` | Use SSL verification for JIRA API requests |
| `listening_address` | String | `0.0.0.0` | Address to listen to |
| `listening_port` | Integer | `5203` | Port to listen to |
| `snooze_url` | String | `http://localhost:5200` | URL to Snooze Web UI (used for links in JIRA descriptions) |
| `message_limit` | Integer | `10` | Maximum number of alerts to process per webhook call |
| `debug` | Boolean | `false` | Show debug logs |

### Example configuration

```yaml
jira_url: https://mycompany.atlassian.net
jira_email: bot@mycompany.com
jira_api_token: ATATT3xFfGF0...
project_key: OPS
issue_type: Task
# issue_type_id: "10001"  # optional, overrides issue_type when set
priority: Medium
priority_mapping:
  emergency: "Critical"
  critical: "High"
  warning: "Medium"
  minor: "Low"
  info: "Lowest"
labels:
  - snooze
  - monitoring
summary_template: "[${severity}] ${host} - ${message}"
description_template: |
  Host: ${host}
  Source: ${source}
  Severity: ${severity}
  Message: ${message}
  Snooze: ${snooze_url}/web/?#/record?tab=All&s=hash%3D${hash}
assignee: "5b109f2e9729b51b54dc274d"    # JIRA account ID or email
reporter: "bot@mycompany.com"              # email-based reporter
custom_fields:
  customfield_10100:
    value: "Infrastructure"
  customfield_10718:
    - id: "11688"
      value: "DevOps 🟣"
reopen_closed: true
reopen_status_name: "To Do"
initial_status: "In Progress"
ssl_verify: true
alert_hash_custom_field: "customfield_10500"
poll_interval: 300
listening_address: 0.0.0.0
listening_port: 5203
snooze_url: https://snooze.mycompany.com
message_limit: 10
debug: false
```

## Issue Type Selection

You can choose the Jira ticket type by name (e.g. `Task`, `Epic`, `Bug`) or by Jira issue type ID.

Resolution order:
1. Payload `issue_type_id`
2. Payload `issue_type`
3. Config `issue_type_id`
4. Config `issue_type`

If `issue_type_id` is set, Jira receives `issuetype.id`. Otherwise, Jira receives `issuetype.name`.

**Payload example (Epic by name):**
```json
{
  "project_key": "OPS",
  "issue_type": "Epic",
  "alert": {{ __self__ | tojson() }}
}
```

**Payload example (type by ID):**
```json
{
  "project_key": "OPS",
  "issue_type_id": "10001",
  "alert": {{ __self__ | tojson() }}
}
```

## How It Works

1. **Alert received**: SnoozeWeb sends a webhook POST to `/alert` on the daemon
2. **New alert**: A new JIRA issue is created with the alert details (host, source, severity, message, etc.)
3. **Re-escalation**: If the alert was already sent previously (tracked via `Inject Response`), a comment is added to the existing JIRA ticket instead of creating a duplicate
4. **Reopen closed tickets** (optional): If `reopen_closed: true` is set and the existing ticket is in a done/closed status, the plugin will automatically transition it back to the configured `reopen_status_name` (default: `To Do`)
5. **Response injection**: The JIRA issue key is returned to SnoozeWeb and stored in the record's `snooze_webhook_responses`, enabling deduplication on subsequent triggers
6. **Status sync (poller)**: A background thread periodically queries JIRA for open tickets with the `alert_hash_custom_field` set. When a ticket is resolved/closed in JIRA, the plugin automatically closes the corresponding Snooze alert via the Snooze API

## Priority Mapping

The `priority_mapping` configuration maps Snooze alert severities to JIRA priority names. When a new ticket is created, the plugin looks up the alert's `severity` field in this mapping to determine the JIRA priority.

**Default mapping:**

| Snooze Severity | JIRA Priority |
|---|---|
| `emergency` | `High` |
| `critical` | `High` |
| `warning` | `Medium` |
| `minor` | `Low` |
| `info` | `Lowest` |

Priority resolution order:
1. Explicit `priority` in webhook payload (per-alert override)
2. `priority_mapping` based on alert severity
3. Default `priority` from configuration

## JIRA API Authentication

This plugin uses [Basic authentication](https://developer.atlassian.com/cloud/jira/platform/basic-auth-for-rest-apis/) with your email and an API token. To create an API token:

1. Go to https://id.atlassian.com/manage-profile/security/api-tokens
2. Click **Create API token**
3. Give it a descriptive label (e.g. "Snooze Bot")
4. Copy the token and add it to your `jira.yaml` configuration

## Custom Fields

The `custom_fields` configuration supports arbitrary JIRA custom fields. Values are passed through directly to the JIRA REST API, so any structure supported by JIRA can be used.

**Simple select field:**
```yaml
custom_fields:
  customfield_10100:
    value: "Infrastructure"
```

**Array of objects (e.g. multi-select or cascading field):**
```yaml
custom_fields:
  customfield_10718:
    - id: "11688"
      value: "DevOps 🟣"
    - id: "11689"
      value: "SRE 🔵"
```

**Multiple custom fields:**
```yaml
custom_fields:
  customfield_10100:
    value: "Infrastructure"
  customfield_10200: "plain string value"
  customfield_10718:
    - id: "11688"
      value: "DevOps 🟣"
```

Custom fields can also be overridden per-alert in the webhook payload:
```json
{
  "project_key": "OPS",
  "custom_fields": {
    "customfield_10100": {"value": "Networking"}
  },
  "alert": {{ __self__ | tojson() }}
}
```

Payload custom fields are merged on top of config defaults (payload wins for same field ID).

## Assignee and Reporter

The `assignee` and `reporter` fields support both JIRA account IDs and email addresses. The plugin auto-detects the format:

- **Account ID** (no `@`): Used directly — `assignee: "5b109f2e9729b51b54dc274d"`
- **Email address** (contains `@`): The plugin calls the JIRA user search API (`/rest/api/3/user/search`) to resolve the email to an `accountId` before creating the issue. Results are cached for the plugin's lifetime.

If an email cannot be resolved to a JIRA user, the field is skipped and a warning is logged.

Both can be overridden per-alert in the webhook payload.

## Initial Status

By default, newly created JIRA issues start in the workflow's default status (typically "To Do" or "Open"). If you set `initial_status`, the plugin will automatically transition the issue to the specified status right after creation.

```yaml
initial_status: "In Progress"
```

The plugin looks up available transitions on the newly created issue, finds the one whose destination status name matches (case-insensitive), and applies it. If no matching transition is found, it falls back to any transition that doesn't lead to a "done" category.

This can also be overridden per-alert in the webhook payload:
```json
{
  "project_key": "OPS",
  "initial_status": "In Review",
  "alert": {{ __self__ | tojson() }}
}
```

## Bidirectional Status Sync (Poller)

When `alert_hash_custom_field` is configured, the plugin stores a Snooze URL in the specified JIRA custom field on each created ticket. A background poller thread uses this field to track open tickets and detect when they are resolved in JIRA.

**How it works:**
1. On issue creation, the plugin writes the Snooze alert URL (e.g. `https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dabc123`) into the configured custom field
2. The poller queries JIRA every `poll_interval` seconds for open issues that have this field set
3. It maintains an in-memory set of tracked open issues
4. When a previously-tracked issue disappears from the open results (i.e. it was resolved/closed in JIRA), the poller extracts the alert hash from the URL and calls the Snooze API to close the corresponding alert

**Setup:**
1. Create a custom text field in your JIRA project (Admin > Issues > Custom fields > Create field > Text Field (single line))
2. Note the field ID (e.g. `customfield_10500`) — you can find it in the JIRA REST API or field configuration
3. Add it to your `jira.yaml`:
   ```yaml
   alert_hash_custom_field: "customfield_10500"
   ```

The poller is enabled by default but only starts if `alert_hash_custom_field` is set. You can disable it explicitly with `poll_enabled: false`.
