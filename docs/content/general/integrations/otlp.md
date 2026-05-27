---
sidebar_position: 18
---

# OpenTelemetry OTLP/HTTP (input)

## Overview

The **snooze-otlp** daemon is a standalone OpenTelemetry receiver that ingests the OTLP **logs** signal over **OTLP/HTTP with JSON encoding** and converts every log record into a Snooze v1 alert. It is a separate process (not an in-process plugin) so it can own a long-lived HTTP listener on the standard OTLP/HTTP port and forward records to `snooze-server` through `pkg/snoozeclient`.

It speaks just enough of the protocol to accept an `ExportLogsServiceRequest` POSTed to `/v1/logs` and is stdlib-only — there is no OpenTelemetry SDK, gRPC, or Protobuf dependency.

:::note

**gRPC OTLP and binary-Protobuf encoding are NOT supported — HTTP + JSON only.** Point your exporter at the HTTP endpoint with `Content-Type: application/json`. A Protobuf content-type is answered with `415 Unsupported Media Type`. Metrics and traces are not mapped in this version (logs only); `/v1/metrics` exists as a no-op stub that accepts and discards the payload.

:::

### What it ingests

Each OTLP `logRecord` becomes one `snoozetypes.Record` with the following field mapping:

| Snooze field | OTLP source |
|----|----|
| `source` | constant `"otlp"` |
| `severity` | `severityNumber` (bucketed, see below), else `severityText` (normalised), else `info` |
| `host` | resource attribute `host.name`, falling back to `service.instance.id` |
| `process` | resource attribute `service.name` |
| `message` | `body.stringValue` (other `body` value types are rendered to a string) |
| `timestamp` | `timeUnixNano` (else the receive time) |
| `raw` | merged resource + log-record attributes, plus the raw `severity_number` / `severity_text` inputs |

### Severity mapping

The OTLP `severityNumber` ranges are bucketed onto Snooze severities:

| OTLP severityNumber | OTLP level | Snooze severity |
|---------------------|------------|-----------------|
| 1–4                 | TRACE      | `debug`         |
| 5–8                 | DEBUG      | `debug`         |
| 9–12                | INFO       | `info`          |
| 13–16               | WARN       | `warning`       |
| 17–20               | ERROR      | `error`         |
| 21–24               | FATAL      | `critical`      |

When `severityNumber` is absent or unspecified (0), `severityText` is normalised instead (e.g. `WARN` → `warning`, `FATAL` → `critical`, unknown custom levels pass through lower-cased). With neither present the default is `info`.

### Listen port

The receiver listens on `:4318` by default — the OpenTelemetry/IANA default port for OTLP/HTTP. Override it with the `listen` config key.

## Configuration

snooze-otlp reads a single YAML file (default `/etc/snooze/otlp.yaml`, override with `-c`). It contains the standard Snooze-client block plus the receiver's own knobs.

``` yaml
# --- Snooze client (where mapped alerts are forwarded) ---
server: https://snooze.example.com
username: ingest
password: change-me
method: local            # auth backend: local | ldap | anonymous
# token: <bearer>        # set to skip the login flow
insecure: false          # disable TLS verification for the Snooze client
request_timeout: 30s     # cap on a single alert POST

# --- OTLP receiver ---
listen: ":4318"          # OTLP/HTTP bind address (default :4318)
debug: false
```

### Field reference

| Key                     | Meaning                                      |
|-------------------------|----------------------------------------------|
| `server`                | Snooze base URL. **Required.**               |
| `username` / `password` | Credentials for the v1 `/login` endpoint.    |
| `method`                | Auth backend; defaults to `local`.           |
| `token`                 | Bearer token; short-circuits login when set. |
| `insecure`              | Skip TLS verification for the Snooze client. |
| `request_timeout`       | Per-request timeout; defaults to `30s`.      |
| `listen`                | OTLP/HTTP bind address; defaults to `:4318`. |
| `debug`                 | Enable debug-level logging.                  |

### Sending logs with curl

POST an `ExportLogsServiceRequest` (OTLP-JSON) to `/v1/logs`:

``` console
$ curl -sS -X POST http://127.0.0.1:4318/v1/logs \
    -H 'Content-Type: application/json' \
    -d '{
      "resourceLogs": [{
        "resource": {"attributes": [
          {"key": "host.name",    "value": {"stringValue": "web-01"}},
          {"key": "service.name", "value": {"stringValue": "checkout"}}
        ]},
        "scopeLogs": [{"logRecords": [{
          "timeUnixNano": "1700000000000000000",
          "severityNumber": 17,
          "severityText": "ERROR",
          "body": {"stringValue": "disk usage at 92%"},
          "attributes": [{"key": "disk.path", "value": {"stringValue": "/var"}}]
        }]}]
      }]
    }'
{}
```

A successful request returns `200 OK` with an empty `ExportLogsServiceResponse` (`{}`). The body may be gzip-compressed — set `Content-Encoding: gzip`.

### Pointing an OpenTelemetry Collector at it

Use the Collector's `otlphttp` exporter with JSON encoding (the `encoding: json` option requires Collector v0.95+). gRPC export is **not** supported by snooze-otlp.

``` yaml
exporters:
  otlphttp/snooze:
    endpoint: http://snooze-otlp-host:4318
    encoding: json
    # The exporter appends the standard /v1/logs path automatically.
    tls:
      insecure: true

service:
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [otlphttp/snooze]
```

### systemd unit

``` ini
[Unit]
Description=Snooze OTLP/HTTP (JSON) log receiver
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-otlp -c /etc/snooze/otlp.yaml
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

## End-to-end test setup

The package ships an env-gated end-to-end test, `TestOTLPE2E`, that POSTs a realistic OTLP-JSON logs payload to a **running** snooze-otlp `/v1/logs` endpoint and asserts a `200 OK`. It is skipped unless its env var is set.

Environment variables read by the e2e test:

| Variable | Meaning |
|----|----|
| `SNOOZE_E2E_OTLP_URL` | Full URL of a live receiver's `/v1/logs` endpoint, e.g. `http://127.0.0.1:4318/v1/logs`. |

Run it against a locally started daemon:

``` console
# Terminal 1 — start the receiver against a test Snooze server
$ snooze-otlp -c /etc/snooze/otlp.yaml

# Terminal 2 — run the end-to-end test
$ export SNOOZE_E2E_OTLP_URL="http://127.0.0.1:4318/v1/logs"
$ go test -run E2E ./internal/components/otlp/...
```

You can also smoke-test without the Go test using the `curl` example above.

## Notes & limitations

- **HTTP + JSON only.** gRPC OTLP and binary-Protobuf encoding are not supported. Protobuf content-types are rejected with `415`.
- **Logs only.** Metrics and traces are not mapped in this version. `/v1/metrics` accepts a JSON body and returns `200 {}` but discards it.
- **Accept-always semantics.** Per the OTLP/HTTP spec the receiver returns `200` once a request has been accepted and decoded; a transient failure forwarding to `snooze-server` is logged but does not change the response, to avoid retry storms from exporters.
- **Severity** is derived from `severityNumber` first (the OTLP-canonical signal), then `severityText`, defaulting to `info`.
- `timeUnixNano` and `intValue` follow the proto3-JSON int64/uint64 rule (quoted strings); the receiver also tolerates bare numbers for robustness.

