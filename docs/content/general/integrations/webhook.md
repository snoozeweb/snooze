---
sidebar_position: 21
---

# Webhook (output)

## Overview

The **webhook** plugin is an in-process Notifier that fires an outbound HTTP request for each matching alert. It is wired as a notification *Action* in the Snooze UI: when an alert matches a Notification rule that references a webhook action, the plugin renders the configured URL, headers, and body as Go `text/template` over the alert record, then dispatches the request.

The plugin is a pure HTTP dispatcher — it owns no database collection and requires no external library. Any endpoint that accepts HTTP (`POST`, `GET`, `PUT`, etc.) can be targeted: REST APIs, Alertmanager silences, external ticketing systems, custom webhooks, etc.

Key capabilities:

- All seven standard HTTP methods are supported.
- Request headers and body are fully templatable.
- Optional Bearer or Basic authentication.
- Optional HTTP/HTTPS/SOCKS proxy support.
- Optional TLS certificate verification bypass for private endpoints.
- `inject_response`: stamp the HTTP response back onto the originating record for downstream use in templates.
- Batching: accumulate multiple alert payloads and POST them as a single JSON array.

## Configuration

Wire the plugin through a **Notification → Action** in the Snooze UI or configuration file. Set the action type to `webhook` and fill the `action_form` fields described below.

### Action fields

| Field | Component | Default | Description |
|----|----|----|----|
| `url` | String | *(required)* | Target URL. May be a Go `text/template` rendered over `.Record` (e.g. `https://api.example.com/alerts/{{ .Record.Host }}`). |
| `method` | Selector | `POST` | HTTP method. Options: `POST`, `GET`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`. |
| `headers` | Arguments | *(optional)* | Key-value pairs of HTTP headers. Header values may be Go `text/template` strings rendered over `.Record`. |
| `body` | Text | *(optional)* | Request body as a Go `text/template` rendered over `.Record` and `.Now`. When empty the plugin sends a JSON-encoded record and sets `Content-Type: application/json` automatically. |
| `proxy` | String | *(optional)* | HTTP/HTTPS/SOCKS proxy URL through which to route the outbound request (e.g. `http://proxy.local:3128`). |
| `tls_insecure` | Switch | `false` | Skip TLS certificate verification when calling HTTPS endpoints. Use only for trusted private endpoints. |
| `inject_response` | Switch | `false` | Parse the HTTP response body (JSON if valid, otherwise raw string) and stamp it onto the originating record under the key `response_<action_name>`. Downstream rules and templates can then access the response. Mutually exclusive with `batch` (the batch path has no single originating record to stamp). |
| `auth` | Object | *(optional)* | Optional authentication block. Supported shapes: `{type: bearer, token: "..."}` or `{type: basic, username: "...", password: "..."}`. |
| `timeout` | String | `10s` | Full request timeout as a Go duration string (e.g. `5s`, `30s`, `2m`). |
| `batch` | Switch | `false` | When enabled, multiple alert bodies are accumulated and POSTed as a single JSON array. Only effective when each rendered body is valid JSON. Ignored when `inject_response` is on. See [Batching](#batching) below. |
| `batch_maxsize` | Number | `100` | Flush the batch when it reaches this many records. |
| `batch_timer` | Number | `10` | Flush the batch when it is at least this many seconds old. |

### Template context

URL, header values, and body are rendered as Go `text/template` with the following top-level data:

- `.Record` — the full alert record (all fields accessible, e.g. `.Record.Host`, `.Record.Severity`).
- `.Now` — the current UTC time (`time.Time`).
- `.ReplyToIDs` — for notifiers that support threading (e.g. Teams), the `message_ids` from a previous `inject_response` stamped onto the record; `null` on first fire.

The template function `tojson` is available to JSON-encode any value: `{{ tojson .Record }}`.

**Python 1.x compatibility.** Templates that used the Jinja2 idioms `{{ __self__ | tojson() }}` or `{{ __self__ }}` are automatically rewritten to `{{ tojson .Record }}` at parse time, so action records ported from 1.x continue to work without manual migration.

### Batching

All three of `batch`, `batch_maxsize`, and `batch_timer` must be configured for batching to activate. If either bound is absent or non-positive the plugin falls back to per-record dispatch.

Only records whose rendered body is valid JSON are eligible for batching. The batch flush POSTs a `[body1, body2, ...]` JSON array to the configured URL.

## Example

``` yaml
url:    "https://hooks.example.com/snooze-alerts"
method: POST
headers:
  X-Api-Key:    "my-api-key"
  X-Host:       "{{ .Record.Host }}"
body: |
  {
    "host":     "{{ .Record.Host }}",
    "severity": "{{ .Record.Severity }}",
    "message":  "{{ .Record.Message }}",
    "time":     "{{ .Now.Format "2006-01-02T15:04:05Z07:00" }}"
  }
timeout: "15s"
```

``` yaml
url:             "https://api.example.com/incidents/{{ .Record.Extra.ticket_id }}"
method:          PATCH
auth:
  type:  bearer
  token: "eyJhbGci..."
body:            '{"status":"triggered","severity":"{{ .Record.Severity }}"}'
inject_response: true
timeout:         "10s"
```

``` yaml
url:           "https://ingest.example.com/bulk"
method:        POST
batch:         true
batch_maxsize: 50
batch_timer:   30
```

## Testing / verifying

1.  **Create a test action** in the Snooze UI (Actions → New → webhook) with `url` pointing at a request-capture service such as `https://webhook.site` or a local `nc -l` listener.

2.  **Create a matching Notification rule** that routes a specific condition (e.g. `source = "test"`) to the new action.

3.  **Send a test alert**:

        $ snooze alert source=test host=test-host severity=info \
            "message=webhook notifier smoke-test"

4.  **Verify the request** arrived at the capture endpoint with the expected headers and body.

To test `inject_response`, inspect the record in the Snooze UI after the notification fires and confirm the `response_<action_name>` field was stamped onto the record.

## Notes & limitations

- The plugin returns an error for any HTTP status code outside the `2xx` range. The notification worker is responsible for retries and dead-letter handling.
- `Content-Type` is set automatically to `application/json` when the rendered body starts with `{` or `[` and no explicit `Content-Type` header is configured.
- `inject_response` and `batch` are mutually exclusive: the batch flush fires after the originating records have been released, so there is no single record to stamp the response onto. When both are set to `true`, `inject_response` wins and batching is disabled for that action.
- The `proxy` field accepts `http://`, `https://`, and `socks5://` URLs. A malformed or scheme-less proxy URL is silently ignored and the request proceeds direct.
- Response bodies are capped at 64 KiB for logging/diagnostics. The first 200 bytes are included in the error message on non-2xx responses.
- The legacy Python `payload` field is accepted as a fallback alias for `body` so that action records ported from Python 1.x continue to work without migration.

