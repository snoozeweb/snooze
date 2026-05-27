---
sidebar_position: 29
---

# Pushover (output)

## Overview

The **Pushover** integration is an in-process Notifier output plugin. It delivers mobile push notifications to Pushover devices via the [Pushover Messages API](https://pushover.net/api). Each matching alert triggers a POST to `https://api.pushover.net/1/messages.json` encoded as `application/x-www-form-urlencoded`; no external daemon is required.

The plugin renders the notification title and message as Go `text/template` expressions evaluated against the alert record, so operators can embed any record field (host, severity, message, source, UID, …) in the push text.

Delivery priority can be set explicitly (`-2` through `2`) or derived automatically from the record's Snooze severity level.

## Configuration

Wire a Pushover action to a notification rule in the Snooze UI under *Notifications → Actions → Add Action → Send a Pushover notification*.

### Field reference

<table>
<colgroup>
<col style="width: 20%" />
<col style="width: 10%" />
<col style="width: 10%" />
<col style="width: 60%" />
</colgroup>
<thead>
<tr>
<th>Field</th>
<th>Required</th>
<th>Default</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>token</code></td>
<td>yes</td>
<td>—</td>
<td>Pushover <strong>application API token</strong> (obtained when you register an app at <a href="https://pushover.net/apps">https://pushover.net/apps</a>). Stored as a Password field.</td>
</tr>
<tr>
<td><code>user</code></td>
<td>yes</td>
<td>—</td>
<td>Pushover <strong>user key</strong> or <strong>group key</strong> shown at the top of <a href="https://pushover.net">https://pushover.net</a> after login.</td>
</tr>
<tr>
<td><code>title</code></td>
<td>no</td>
<td><code>{{ .Severity }} on {{ .Host }}</code></td>
<td>Notification title. Go <code>text/template</code> over the alert record fields: <code>.UID</code>, <code>.Host</code>, <code>.Source</code>, <code>.Process</code>, <code>.Severity</code>, <code>.Message</code>, <code>.State</code>, <code>.Timestamp</code>, <code>.Tags</code>.</td>
</tr>
<tr>
<td><code>message</code></td>
<td>no</td>
<td><code>{{ .Message }}</code></td>
<td>Notification body. Same template context as <code>title</code>.</td>
</tr>
<tr>
<td><p><code>priority</code></p></td>
<td><p>no</p></td>
<td><p><code>auto</code></p></td>
<td><p>Delivery priority. <code>auto</code> maps the record severity:</p>
<ul>
<li><code>emergency</code> / <code>critical</code> → 2 (emergency — adds <code>retry=60</code> and <code>expire=3600</code> automatically)</li>
<li><code>error</code> / <code>err</code> → 1 (high)</li>
<li><code>warning</code> / <code>warn</code> → 0 (normal)</li>
<li>anything else (<code>info</code>, <code>notice</code>, <code>debug</code>) → −1 (low)</li>
</ul>
<p>Explicit values: <code>-2</code> (lowest), <code>-1</code> (low), <code>0</code> (normal), <code>1</code> (high), <code>2</code> (emergency).</p></td>
</tr>
<tr>
<td><code>sound</code></td>
<td>no</td>
<td><em>(user default)</em></td>
<td>Pushover sound name, e.g. <code>alien</code>, <code>bike</code>, <code>classical</code>, <code>none</code>. Leave empty to use the user's default device sound.</td>
</tr>
<tr>
<td><code>url</code></td>
<td>no</td>
<td>—</td>
<td>A supplementary URL attached to the notification (e.g. a Grafana dashboard link).</td>
</tr>
<tr>
<td><code>url_title</code></td>
<td>no</td>
<td>—</td>
<td>Display title for <code>url</code>. Ignored when <code>url</code> is empty.</td>
</tr>
<tr>
<td><code>api_base</code></td>
<td>no</td>
<td><code>https://api.pushover.net</code></td>
<td>Override the Pushover API base URL. Used for testing; leave at the default in production.</td>
</tr>
<tr>
<td><code>timeout</code></td>
<td>no</td>
<td><code>10s</code></td>
<td>Per-request HTTP timeout as a Go duration string (e.g. <code>5s</code>, <code>30s</code>).</td>
</tr>
</tbody>
</table>

``` yaml
token: "aaaBBBcccDDD111222333eeeFFF444"     # your app token
user: "uUUvVVwWWxXX1122334455aabbccdd"       # your user/group key
title: "{{ .Severity }} on {{ .Host }}"
message: "{{ .Message }}"
priority: auto
sound: ""
url: ""
url_title: ""
api_base: "https://api.pushover.net"
timeout: "10s"
```

## End-to-end test setup

To run the live integration test you need:

1.  A **Pushover account** at <https://pushover.net> and at least one registered device.
2.  A **Pushover application token** — register a new app at <https://pushover.net/apps/build> and copy the *API Token/Key*.
3.  Your **Pushover user key** — shown on your dashboard at <https://pushover.net> immediately after login.

Export the two variables and run the E2E test:

``` console
$ export SNOOZE_E2E_PUSHOVER_TOKEN="<your-app-token>"
$ export SNOOZE_E2E_PUSHOVER_USER="<your-user-or-group-key>"
$ go test -run E2E ./internal/pluginimpl/pushover/...
```

When either variable is unset the test is skipped automatically.

### Environment variables

| Variable | Description |
|----|----|
| `SNOOZE_E2E_PUSHOVER_TOKEN` | Pushover application API token (see *App token* above). |
| `SNOOZE_E2E_PUSHOVER_USER` | Pushover user key or group key. |

## Notes & limitations

- **Priority 2 (emergency)** requires `retry` and `expire` parameters. The plugin automatically sends `retry=60` (retry every 60 s) and `expire=3600` (give up after 1 h) when the resolved priority is 2. These values are not currently configurable via the action_form; open an issue if you need to override them.
- **Group keys** are supported by the Pushover API as the `user` field; they work transparently with this plugin.
- **HTML in messages**: the Pushover API supports limited HTML formatting (`<b>`, `<i>`, `<a>`). You can include these tags in your `message` template; the plugin sends the body verbatim.
- **Rate limits**: Pushover enforces a per-application monthly message quota (7500 messages/month on the free tier). See <https://pushover.net/api#limits> for current limits.
- **No resolve/close path**: Pushover notifications are fire-and-forget; the plugin does not take special action when `rec.State == "close"`.

