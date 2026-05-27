---
sidebar_position: 31
---

# Twilio (output)

## Overview

The **Twilio** plugin is an in-process output (Notifier) plugin that delivers Snooze alerts via the [Twilio REST API](https://www.twilio.com/docs/usage/api). Two delivery modes are supported:

- **SMS** (default): posts a text message to each recipient using the `Messages` resource (`POST .../2010-04-01/Accounts/{AccountSID}/Messages.json`).
- **Voice**: places an automated phone call to each recipient using the `Calls` resource (`POST .../2010-04-01/Accounts/{AccountSID}/Calls.json`) with a TwiML `<Response><Say>…</Say></Response>` document constructed from the rendered `voice_message` template.

Authentication is HTTP Basic auth using the Twilio Account SID as the username and the Auth Token as the password, exactly as documented in the Twilio REST API.

Multiple recipients are supported: set `to` to a comma-separated list of E.164 phone numbers. One Twilio API request is made per recipient; if any individual delivery fails the plugin returns an aggregated error after attempting all recipients.

## Configuration

Wire the Twilio plugin to an alert by adding a **Notification → Action** of type *Send a Twilio SMS or voice call* in the Snooze UI, then fill in the action form fields described below.

### Field reference

| Field | Required | Description |
|----|----|----|
| `account_sid` | yes | Twilio Account SID (`ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`), found on the [Twilio Console](https://console.twilio.com) dashboard. |
| `auth_token` | yes | Twilio Auth Token (stored as a password field), also found on the Console dashboard. |
| `from` | yes | The Twilio phone number that sends the message or places the call, in [E.164 format](https://www.twilio.com/docs/glossary/what-e164) (e.g. `+15005550006`). Must be a Twilio-owned or verified number. |
| `to` | yes | Comma-separated list of destination phone numbers in E.164 format (e.g. `+15005550007,+15005550008`). One Twilio API request is made per entry; partial failures are aggregated and returned together. |
| `mode` | no | `sms` (default) or `voice`. |
| `message` | no | SMS body text. Go `text/template` rendered against the record. Default: `{{ .Severity }} on {{ .Host }}: {{ .Message }}`. Used only in `sms` mode. |
| `voice_message` | no | Text spoken via TwiML `<Say>` in `voice` mode. Go `text/template` rendered against the record; XML-special characters (`& < > " '`) are automatically escaped. Default: `Snooze alert. {{ .Severity }} on {{ .Host }}. {{ .Message }}`. |
| `api_base` | no | Twilio REST API base URL. Default: `https://api.twilio.com`. Override for testing or private Twilio deployments. |
| `timeout` | no | HTTP request timeout as a Go duration string (e.g. `15s`, `1m`). Default: `10s`. |

``` yaml
account_sid: ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
auth_token:  <your auth token>
from:        "+15005550006"
to:          "+15005550007,+15005550008"
mode:        sms
message:     "{{ .Severity }} alert on {{ .Host }}: {{ .Message }}"
timeout:     10s
```

## End-to-end test setup

The package ships an env-gated end-to-end test (`TestTwilioE2E`) that sends one real SMS against the live Twilio API. It is skipped by default and requires four environment variables.

**Prerequisites**

1.  Create a [Twilio account](https://www.twilio.com/try-twilio) (a free trial account is sufficient).
2.  In the [Twilio Console](https://console.twilio.com), note your **Account SID** and **Auth Token** from the dashboard.
3.  Buy or provision a Twilio phone number (trial accounts receive one during sign-up). Copy it in E.164 format (e.g. `+15005550006`).
4.  On a trial account, the destination number (`to`) must first be added to the *Verified Caller IDs* list in the Console. Paid accounts can send to any E.164 number.

**Running the test**

``` console
$ export SNOOZE_E2E_TWILIO_SID="ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
$ export SNOOZE_E2E_TWILIO_TOKEN="<auth token from console>"
$ export SNOOZE_E2E_TWILIO_FROM="+15005550006"
$ export SNOOZE_E2E_TWILIO_TO="+15005550007"
$ go test -run TestTwilioE2E ./internal/pluginimpl/twilio/...
```

| Environment variable      | Purpose                        |
|---------------------------|--------------------------------|
| `SNOOZE_E2E_TWILIO_SID`   | Twilio Account SID             |
| `SNOOZE_E2E_TWILIO_TOKEN` | Twilio Auth Token              |
| `SNOOZE_E2E_TWILIO_FROM`  | Sending phone number (E.164)   |
| `SNOOZE_E2E_TWILIO_TO`    | Recipient phone number (E.164) |

## Notes & limitations

**E.164 formatting**  
All phone numbers (`from` and each entry in `to`) must be in [E.164 format](https://www.twilio.com/docs/glossary/what-e164): a `+` followed by the country code and subscriber number, no spaces or dashes (e.g. `+15005550007`). Twilio rejects incorrectly formatted numbers with HTTP 400 / error code 21211.

**Trial-account restrictions**  
Twilio trial accounts can only send to phone numbers that have been added to the *Verified Caller IDs* list in the Console. To send to arbitrary numbers, upgrade to a paid account.

**Voice TwiML**  
The voice mode uses a minimal `<Response><Say>…</Say></Response>` TwiML document. More complex TwiML (e.g. `<Gather>` for DTMF acknowledgement) is not supported by this plugin; use the webhook plugin with a TwiML Bin or a TwiML App if you need that.

**No gRPC / Conversations API**  
This plugin uses only the Twilio Programmable Messaging and Voice REST APIs. The Twilio Conversations, Verify, or Notify APIs are out of scope.

**Rate limits**  
Twilio applies per-account and per-number message throughput limits. For high-volume alerting, review the [Twilio messaging limits](https://support.twilio.com/hc/en-us/articles/115002943027) and consider upgrading your account or provisioning additional numbers.

