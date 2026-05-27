---
sidebar_position: 34
---

# ServiceNow (output)

## Overview

The **servicenow** plugin is an in-process Notifier that opens ServiceNow incidents — and optionally resolves them — via the [ServiceNow Table REST API](https://developer.servicenow.com/dev.do#!/reference/api/sandiego/rest/c_TableAPI).

- **Create** (firing alert): the plugin POSTs a new row to the configured table (default `incident`) with `short_description`, `description`, `urgency`, `impact`, `category`, and a `correlation_id` derived from the record hash (or UID when no hash is available).
- **Resolve** (`rec.State == "close"`): the plugin GETs the table to find the `sys_id` matching the `correlation_id`, then PATCHes that row to `state = 6` (Resolved) with a `close_code` and `close_notes`. If no matching record is found the resolve call is a no-op (logged at info level).

Authentication is HTTP Basic (`Authorization: Basic <base64(user:pass)>`). The plugin uses `net/http` only — no ServiceNow SDK or external library is required.

## Configuration

Wire the plugin through a **Notification → Action** in the Snooze UI or configuration file. Set the action type to `Create a ServiceNow incident` and fill the `action_form` fields described below.

### Field reference

| Field | Default | Description |
|----|----|----|
| `instance_url` | *(required)* | ServiceNow instance base URL, e.g. `https://dev12345.service-now.com`. No trailing slash. |
| `username` | *(required)* | ServiceNow user with the `itil` and `rest_api_explorer` roles. |
| `password` | *(required)* | Password for the ServiceNow user. Stored as a `Password` component (masked in the UI). |
| `table` | `incident` | ServiceNow table to write records to. Change to `em_event` or another table if your ServiceNow instance routes events differently. |
| `urgency` | `auto` | Incident urgency. `auto` derives from the Snooze severity: `critical`/`emergency` → `1` (High); `error`/`warning` → `2` (Medium); anything else → `3` (Low). Override with an explicit `1`, `2`, or `3`. |
| `impact` | `auto` | Incident impact. Same `auto` mapping as `urgency`. |
| `category` | *(optional)* | Incident category string (e.g. `software`, `hardware`). Leave blank to omit the field from the create payload. |
| `caller_id` | *(optional)* | `sys_id` or `user_name` of the ServiceNow user to set as the incident caller. Leave blank to omit. |
| `timeout` | `10s` | HTTP request timeout as a Go duration string (e.g. `5s`, `30s`). Applied independently to the create POST and to each of the lookup GET and resolve PATCH during a close event. |

``` yaml
instance_url: "https://dev12345.service-now.com"
username:     "snooze-notifier"
password:     "supersecret"
table:        "incident"
urgency:      "auto"
impact:       "auto"
category:     "software"
caller_id:    ""
timeout:      "10s"
```

### Severity → urgency / impact mapping

When `urgency` or `impact` is set to `auto` (the default), the value is derived from the alert record's `severity` field:

| Snooze severity                        | Urgency / Impact | ServiceNow label |
|----------------------------------------|------------------|------------------|
| `critical`, `emergency`                | `1`              | High             |
| `error`, `err`, `warning`, `warn`      | `2`              | Medium           |
| `notice`, `info`, `debug`, *(unknown)* | `3`              | Low              |

### Resolve flow

When a record arrives with `state: close`, the plugin performs two HTTP calls:

1.  **GET** `{instance_url}/api/now/table/{table}?sysparm_query=correlation_id={id}&sysparm_limit=1` to retrieve the `sys_id` of the matching incident.
2.  **PATCH** `{instance_url}/api/now/table/{table}/{sys_id}` with `{"state":"6","close_code":"Resolved","close_notes":"..."}`.

If step 1 returns an empty result list, both calls are skipped and no error is returned (the event is logged at info level). This handles the case where a resolution event arrives before the corresponding create was processed or when a manual agent already closed the incident.

## End-to-end test setup

The end-to-end test creates a real incident on a running ServiceNow instance and asserts that no error is returned. A free [Personal Developer Instance (PDI)](https://developer.servicenow.com/dev.do#!/guides/washingtondc/now-platform/tpb-guide/personal-developer-instances) is sufficient.

**1. Provision a PDI**

1.  Sign in at `https://developer.servicenow.com`.
2.  Click **Request Instance** and wait for your PDI to become available.
3.  Note the instance URL (e.g. `https://dev12345.service-now.com`).

**2. Create a dedicated user (recommended)**

1.  Navigate to **User Administration → Users** in your PDI.
2.  Create a new user (e.g. `snooze-e2e`) with a secure password.
3.  Assign the `itil` and `rest_api_explorer` roles to the user.

**3. Export the environment variables**

``` console
$ export SNOOZE_E2E_SERVICENOW_INSTANCE="https://dev12345.service-now.com"
$ export SNOOZE_E2E_SERVICENOW_USER="snooze-e2e"
$ export SNOOZE_E2E_SERVICENOW_PASSWORD="supersecret"
```

**4. Run the test**

``` console
$ go test -v -run TestServiceNowE2E ./internal/pluginimpl/servicenow/...
```

The test posts one incident to the `incident` table. Check **Incident → All** in the ServiceNow UI to verify that the record was created with the expected fields.

**Environment variables read by the e2e test:**

| Variable | Purpose |
|----|----|
| `SNOOZE_E2E_SERVICENOW_INSTANCE` | ServiceNow instance base URL (e.g. `https://dev12345.service-now.com`). The test is skipped when this or any of the other two variables are unset. |
| `SNOOZE_E2E_SERVICENOW_USER` | Username of the ServiceNow user with `itil` and `rest_api_explorer` roles. |
| `SNOOZE_E2E_SERVICENOW_PASSWORD` | Password for the ServiceNow user. |

## Notes & limitations

- Only the `incident` table is tested in CI. Other tables (`em_event`, `problem`, `change_request`) should work but are not exercised by the unit tests.
- The `close_code` used during resolve is hard-coded to `"Resolved"`. Some ServiceNow configurations enforce a specific close-code vocabulary; if your instance rejects this value, adjust the action by filing a configuration request with your ServiceNow administrator.
- The plugin does not implement client-side rate limiting or automatic retries. The Snooze notification worker is responsible for retry and dead-letter handling.
- HTTPS is strongly recommended. Plain HTTP connections will transmit Basic auth credentials in the clear.
- The Table API requires the `rest_api_explorer` role in addition to `itil`. Missing either role causes `HTTP 403`, surfaced as an error by the plugin.
- ServiceNow PDIs hibernate after inactivity. If the e2e test returns a connection error, log in to the developer portal and wake your instance first.

