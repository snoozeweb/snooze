---
sidebar_position: 11
---

# Kapacitor (input)

## Overview

The **kapacitor** plugin is an in-process WebhookReceiver that accepts [Kapacitor HTTP alert handler](https://docs.influxdata.com/kapacitor/v1/reference/event_handlers/http/) payloads and converts them into Snooze records. It is registered at `/api/v1/webhook/kapacitor`.

Kapacitor's `http()` alert handler POSTs a JSON envelope containing an alert `id`, `message`, `level`, `time`, and an InfluxDB-style `data.series[]` block. The plugin fans out across the `data.series` array producing one Snooze record per series, which means a Kapacitor alert that matches multiple hosts in a single query produces a separate record for each one.

## Configuration

### Inbound URL

The plugin mounts at:

    /api/v1/webhook/kapacitor

No authentication is required on this endpoint by default. See [Integrations](./index.md) and [Ingest configuration](../../configuration/ingest.md) for hardening options including a shared ingest token (`config.ingest.token`).

### Configuring the Kapacitor http() handler

In your TICK script, add an `http()` alert handler pointing to the Snooze endpoint:

``` text
stream
  |from()
    .measurement('cpu')
  |alert()
    .crit(lambda: "usage_idle" < 10)
    .warn(lambda: "usage_idle" < 20)
    .id('cpu_alert/{{ index .Tags "host" }}')
    .message('CPU critical on {{ index .Tags "host" }}: {{ .Level }}')
    .http('https://<snooze-host>/api/v1/webhook/kapacitor')
```

The handler sends a POST for every state transition (CRITICAL, WARNING, INFO, OK) as well as periodic keepalive messages. Keepalive messages with an empty `data.series` are acknowledged by Snooze with a `{"received":0}` response and do not create records.

To expose Snooze-specific metadata, add `severity`, `process`, or `host` tags to the series in InfluxDB; the plugin pops these from `data.series[].tags` and maps them to the corresponding record fields.

### Curl example

Post a representative payload with one series:

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/kapacitor \
    -H 'Content-Type: application/json' \
    -d '{
      "id": "cpu_alert/web-1",
      "message": "CPU critical on web-1: CRITICAL",
      "details": "",
      "time": "2024-01-15T12:00:00Z",
      "duration": 0,
      "level": "CRITICAL",
      "data": {
        "series": [
          {
            "name": "cpu",
            "tags": {
              "host": "web-1",
              "process": "nginx",
              "env": "prod"
            },
            "columns": ["time", "usage_idle"],
            "values": [
              ["2024-01-15T12:00:00Z", 7.3]
            ]
          }
        ]
      },
      "tags": {"datacenter": "eu-west-1"}
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

To test a recovery (`level=OK` sets `State="close"`):

``` console
$ curl -s -X POST https://<snooze-host>/api/v1/webhook/kapacitor \
    -H 'Content-Type: application/json' \
    -d '{
      "id": "cpu_alert/web-1",
      "message": "CPU recovered on web-1",
      "time": "2024-01-15T13:00:00Z",
      "level": "OK",
      "data": {
        "series": [
          {
            "name": "cpu",
            "tags": {"host": "web-1", "process": "nginx"}
          }
        ]
      }
    }'
```

Expected response:

    {"accepted":1,"received":1,"status":"ok"}

### Field mapping

One Snooze record is produced per `data.series[]` entry. The `severity`, `host`, and `process` tag keys are popped from the series tags before the remaining keys are collected into `Record.Tags`.

<table>
<colgroup>
<col style="width: 25%" />
<col style="width: 20%" />
<col style="width: 55%" />
</colgroup>
<thead>
<tr>
<th>Payload field</th>
<th>Snooze field</th>
<th>Notes</th>
</tr>
</thead>
<tbody>
<tr>
<td><code>Source</code> (constant)</td>
<td><code>Source</code></td>
<td>Always <code>"kapacitor"</code>.</td>
</tr>
<tr>
<td><code>data.series[i].tags.host</code></td>
<td><code>Host</code></td>
<td>Falls back to <code>data.series[i].tags.instance</code>. Empty string when neither is set.</td>
</tr>
<tr>
<td><code>data.series[i].tags.severity</code></td>
<td><code>Severity</code></td>
<td>When present, used verbatim (takes priority over <code>level</code>). Otherwise the envelope <code>level</code> is normalised: <code>CRITICAL</code> → <code>"critical"</code>; <code>WARNING</code> → <code>"warning"</code>; <code>INFO</code> or <code>OK</code> → <code>"info"</code>. Unknown levels are lowercased and passed through. Defaults to <code>"critical"</code> when <code>level</code> is absent.</td>
</tr>
<tr>
<td><code>message</code></td>
<td><code>Message</code></td>
<td>The human-readable alert message from the Kapacitor TICKscript.</td>
</tr>
<tr>
<td><code>data.series[i].tags.process</code></td>
<td><code>Process</code></td>
<td>Falls back to the envelope <code>id</code> when the tag is absent.</td>
</tr>
<tr>
<td><code>time</code></td>
<td><code>Timestamp</code></td>
<td>Falls back to server <code>time.Now()</code> when absent or zero.</td>
</tr>
<tr>
<td><code>data.series[i].tags</code> (remaining, after popping host/process/severity)
<ul>
<li><code>tags</code> (envelope)</li>
</ul></td>
<td><code>Tags</code></td>
<td>Sorted, deduplicated union of the remaining series tag keys and the envelope <code>tags</code> keys.</td>
</tr>
<tr>
<td><code>level</code></td>
<td><code>State</code></td>
<td><code>"OK"</code> (after upper-casing) → <code>State="close"</code>; all other levels leave <code>State</code> empty.</td>
</tr>
<tr>
<td>Full envelope</td>
<td><code>Raw</code></td>
<td>The complete decoded payload is JSON-round-tripped into <code>Raw</code>, including <code>data.series</code> with all columns and values.</td>
</tr>
</tbody>
</table>

## Notes & limitations

- **Unauthenticated by default.** The endpoint accepts any POST from any source. Restrict access at the network layer and/or configure a shared ingest token — see `config.ingest` / [Ingest configuration](../../configuration/ingest.md) and [Integrations](./index.md).
- **Series fan-out.** A Kapacitor batch alert that matches multiple hosts produces one Snooze record per series. Each record carries only its own series data in `Raw`, not the full batch.
- **Keepalive payloads.** Kapacitor periodically sends payloads with an empty `data.series` array to confirm liveness. These are acknowledged with HTTP 200 and `{"received":0}` and do not create records.
- **Tag-based metadata.** Host, process, and severity must be exposed as InfluxDB measurement tags or computed in the TICKscript and attached to the series data. Without a `host` tag, the `Host` field is left empty.
- **Level normalisation vs. severity tag.** The per-series `severity` tag always wins over the envelope `level`. Use this to override the Kapacitor level vocabulary (CRITICAL/WARNING/INFO/OK) with a Snooze-vocabulary value for a specific metric.

