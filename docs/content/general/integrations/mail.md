---
sidebar_position: 20
---

# Mail / SMTP (output)

## Overview

The **mail** plugin is an in-process Notifier that delivers alert notifications as SMTP email messages. It is wired as a notification *Action* in the Snooze UI: when an alert matches a Notification rule that references a mail action, the plugin connects to the configured SMTP server and sends one email per alert (or one batched email covering multiple alerts when batching is enabled).

Both plain-text and HTML email formats are supported. Subject and body are Go `text/template` (or `html/template` for HTML mode) rendered over the alert record, so you can embed any record field directly in the message.

Three SMTP transport modes are available: plain, STARTTLS, and implicit TLS (`tls`). Optional SMTP PLAIN authentication is supported.

## Configuration

Wire the plugin through a **Notification → Action** in the Snooze UI or configuration file. Set the action type to `mail` and fill the `action_form` fields described below.

### Action fields

| Field | Component | Default | Description |
|----|----|----|----|
| `host` | String | `localhost` | SMTP server hostname or IP address. |
| `port` | Number | `25` | SMTP server port. |
| `from` | String | *(empty)* | Envelope sender address and `From:` header value. |
| `to` | Text | *(required)* | Comma-separated list of primary recipient addresses. |
| `cc` | Text | *(optional)* | Comma-separated list of Cc recipient addresses. |
| `bcc` | Text | *(optional)* | Comma-separated list of Bcc recipient addresses. Addresses appear only in the SMTP envelope, not in the message headers. |
| `priority` | Selector | `3` (Normal) | `X-Priority` header value. Options: `1` Highest, `2` High, `3` Normal, `4` Low, `5` Lowest. |
| `subject` | String | `Alert: {{ .Host }}` | Email subject rendered as a Go `text/template` over the alert record. |
| `message` | Text | `Message: {{ .Message }}` | Email body rendered as a Go `text/template` (plain mode) or `html/template` (HTML mode) over the alert record. When empty the plugin falls back to a multi-line default template that prints host, source, process, severity, and message. |
| `type` | Radio | `plain` | Email format. `plain` → `text/plain`; `html` → `text/html`. |
| `tls_mode` | Selector | `none` | SMTP transport security. `none` → plain TCP; `starttls` → upgrade via STARTTLS after connect; `tls` → implicit TLS (SMTPS, typically port 465). |
| `username` | String | *(optional)* | SMTP AUTH PLAIN username. Leave empty to skip authentication. |
| `password` | Password | *(optional)* | SMTP AUTH PLAIN password. |
| `timeout` | Number | `10` | SMTP dial and overall transaction timeout in seconds. |
| `batch` | Switch | `false` | When enabled, multiple matching alerts are accumulated and flushed in a single email instead of one email per alert. The subject is taken from the first queued alert; bodies are joined with a separator. See [Batching](#batching) below. |
| `batch_maxsize` | Number | `100` | Flush the batch when it reaches this many alerts. |
| `batch_timer` | Number | `10` | Flush the batch when it is at least this many seconds old. |

### Template variables

Both `subject` and `message` are Go templates executed against the alert record. The following fields are available directly (e.g. `{{ .Host }}`):

`.UID`, `.Host`, `.Source`, `.Process`, `.Severity`, `.Message`, `.State`, `.Timestamp`, `.Tags`, `.Environment`.

Any extra fields stored in the record are accessible via `.Extra` (a `map[string]any`).

### Batching

All three of `batch`, `batch_maxsize`, and `batch_timer` must be configured for batching to activate. If either bound is absent or non-positive, the plugin silently falls back to one email per alert.

Each time an alert triggers the action it is queued. The queue is flushed on whichever comes first: the queue reaching `batch_maxsize` records, or `batch_timer` seconds elapsing since the oldest queued alert.

## Example

``` yaml
host:     "smtp.example.com"
port:     587
from:     "snooze-alerts@example.com"
to:       "ops-team@example.com"
cc:       "noc@example.com"
subject:  "[{{ .Severity | upper }}] {{ .Host }}: {{ .Message }}"
message:  |
  Alert received by Snooze.

  Host:     {{ .Host }}
  Source:   {{ .Source }}
  Severity: {{ .Severity }}
  Message:  {{ .Message }}
  Time:     {{ .Timestamp }}
type:     plain
tls_mode: starttls
username: "snooze-alerts@example.com"
password: "s3cr3t"
timeout:  15
```

``` yaml
host:          "smtp.example.com"
port:          465
from:          "snooze@example.com"
to:            "ops@example.com"
subject:       "[Snooze] Alert on {{ .Host }}"
message:       "<h2>{{ .Severity }}</h2><p>{{ .Message }}</p>"
type:          html
tls_mode:      tls
batch:         true
batch_maxsize: 50
batch_timer:   30
```

## Testing / verifying

1.  **Create a test action** in the Snooze UI (Actions → New → mail) with `to` set to your own address and the remaining fields pointing at an accessible SMTP server.

2.  **Create a matching Notification rule** that routes a specific condition (e.g. `source = "test"`) to the new action.

3.  **Send a test alert** via the REST API or CLI:

        $ snooze alert source=test host=test-host severity=info \
            "message=mail notifier smoke-test"

4.  **Verify delivery** by checking the recipient's inbox. If the email does not arrive within the configured `timeout`, check the snooze-server logs for SMTP errors.

To exercise STARTTLS or TLS transport you can use `swaks` or `openssl s_client` to confirm the SMTP server supports the chosen mode before committing the action config.

## Notes & limitations

- At least one of `to`, `cc`, or `bcc` must be non-empty; the plugin returns an error and does not attempt delivery otherwise.
- MIME multipart is not used. The message carries a single body part. Attachments are not supported.
- The `bcc` addresses appear only in the SMTP `RCPT TO` command, not in any message header, which is the standard Bcc behaviour.
- STARTTLS negotiation will fail if the server does not advertise the `STARTTLS` extension; the plugin surfaces this as an error rather than silently downgrading to plain transport.
- SMTP AUTH uses PLAIN only. If the server requires a different mechanism (e.g. LOGIN, CRAM-MD5) the authentication step will fail.
- The `timeout` covers the full dial-plus-transaction time. Very large batched messages or slow SMTP servers may require a higher value.
- The Python 1.x action form did not expose `cc`, `bcc`, `tls_mode`, `username`, `password`, or `timeout`. These are Go-port additions and have no Python equivalent.

