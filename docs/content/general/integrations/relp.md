---
sidebar_position: 14
---

# RELP (input)

## Overview

**snooze-relp** is a standalone daemon that accepts syslog messages over RELP (Reliable Event Logging Protocol) — rsyslog's TCP-based transport that guarantees delivery by ACKing each frame only after the payload has been durably accepted. It is a separate process that owns a TCP listener (by default on port 2514) and forwards parsed records to `snooze-server` via `pkg/snoozeclient`.

RELP wraps syslog payload in typed frames. `snooze-relp` decodes the RELP framing, extracts the `syslog` command payload, and then hands the raw syslog line to the same RFC 3164 / RFC 5424 parser used by [Syslog (input)](./syslog.md). The RELP ACK (`rsp 200 OK`) is sent only after `PostAlert` returns, honouring the at-least-once delivery contract. A failed forward produces a NACK so rsyslog retries the frame.

### What it ingests

Each RELP `syslog` frame becomes one `snoozetypes.Record`. The field mapping is identical to [Syslog (input)](./syslog.md):

| Snooze field          | Source                                |
|-----------------------|---------------------------------------|
| `source`              | constant `"syslog"`                   |
| `host`                | syslog `HOSTNAME` field               |
| `process`             | `APPNAME` (or `PROCID` as fallback)   |
| `severity`            | syslog severity number mapped to name |
| `message`             | syslog message body                   |
| `timestamp`           | syslog timestamp (or receive time)    |
| `raw.format`          | `"rfc3164"` or `"rfc5424"`            |
| `raw.facility`        | syslog facility name                  |
| `raw.original`        | raw syslog line                       |
| `raw.peer`            | remote IP:port from the RELP session  |
| `raw.structured_data` | RFC 5424 SD-ELEMENTs (when present)   |

## Configuration

`snooze-relp` reads `/etc/snooze/relp.yaml` by default. Override the path with `-c` or the `SNOOZE_RELP_CONFIG` environment variable.

``` yaml
# --- Snooze server (where alerts are POSTed) ---
server: "https://snooze.example.com"    # Required
username: "ingest"
password: "change-me"
method: "local"           # auth backend: local | ldap | anonymous
# token: ""               # bearer token (skips username/password)
insecure: false           # disable TLS verification for the Snooze client

# --- RELP listener ---
listen: "0.0.0.0:2514"   # TCP bind address (default: 0.0.0.0:2514)

# --- Parser ---
parser: "auto"            # auto | rfc3164 | rfc5424 (default: auto)

# --- Tuning ---
request_timeout: 10s      # per-alert POST timeout (default: 10s)
max_frame_bytes: 1048576  # max accepted RELP frame size in bytes (default: 1 MiB)
read_timeout: 0s          # per-session idle deadline; 0 disables (default: 0)
```

### Field reference

| Key | Meaning |
|----|----|
| `server` | Snooze base URL. **Required.** |
| `username` / `password` | Credentials for the v1 `/login` endpoint. |
| `method` | Auth backend; defaults to `local`. |
| `token` | Bearer token; short-circuits login when set. |
| `insecure` | Skip TLS verification for the Snooze client. |
| `listen` | TCP bind address; defaults to `0.0.0.0:2514`. |
| `parser` | Syslog format: `auto` (default), `rfc3164`, or `rfc5424`. |
| `request_timeout` | Per-request timeout; defaults to `10s`. |
| `max_frame_bytes` | Maximum RELP frame size; defaults to `1048576` (1 MiB). Frames larger than this limit are rejected with a NACK to prevent memory exhaustion. |
| `read_timeout` | Per-session idle deadline between frames. `0` (the default) disables the timeout. |

### systemd unit

``` ini
[Unit]
Description=Snooze RELP ingestion daemon
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-relp -c /etc/snooze/relp.yaml
Restart=on-failure
RestartSec=5s

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

Port 2514 is unprivileged so no `AmbientCapabilities` are needed. If you bind port 514 instead, add the same `CAP_NET_BIND_SERVICE` block as the syslog unit.

## Usage

### rsyslog (recommended)

RELP is rsyslog's own reliable-delivery module. Enable the `omrelp` output module and point it at `snooze-relp`:

``` text
module(load="omrelp")

# Forward all messages reliably to snooze-relp
*.* action(type="omrelp"
           target="snooze-host"
           port="2514"
           template="RSYSLOG_SyslogProtocol23Format")
```

After editing:

``` console
$ sudo systemctl restart rsyslog
```

The `template="RSYSLOG_SyslogProtocol23Format"` argument sends RFC 5424 messages. Omit it (or use `RSYSLOG_TraditionalSyslogFormat`) for RFC 3164.

### syslog-ng

syslog-ng supports RELP via the `relp()` destination driver:

``` text
destination d_snooze_relp {
    relp("snooze-host" port(2514));
};
log { source(s_src); destination(d_snooze_relp); };
```

## Testing / verifying

Use the `relpcat` tool (part of `librelp-utils`) to send a single frame:

``` console
$ echo '<165>1 2024-01-15T10:00:00Z web-01 myapp 12345 - - test RELP message' \
    | relpcat -h snooze-host -p 2514
```

Alternatively, generate a minimal RELP session with netcat (for quick smoke tests only — real RELP framing requires the `txnr octets command data LF` wire format):

``` console
# Open the session (open frame), send one syslog frame, close
$ (
  printf '1 2 open 28\nrelp_version=0\ncommands=syslog\n'
  printf '2 6 syslog 58\n<13>Jan 15 10:00:00 web-01 myapp[1]: hello snooze\n'
  printf '3 5 close 0\n'
) | nc snooze-host 2514
```

Confirm the record arrived:

``` console
$ curl -sS -H 'Authorization: Bearer <token>' \
    'https://snooze.example.com/api/v1/record' \
    | jq '.[] | select(.source=="syslog") | {host,process,message}'
```

## Notes & limitations

- **At-least-once delivery.** `snooze-relp` ACKs a frame only after `PostAlert` returns successfully. If the Snooze server is unreachable the daemon returns a NACK, causing rsyslog to queue and retry. This is the key advantage over plain syslog UDP/TCP.
- **TLS / STARTTLS is not supported.** The current implementation negotiates only the `syslog` command in the RELP open handshake. `relp_tls` or `compression` client offers are silently ignored. Use a TLS-terminating proxy in front of snooze-relp if encryption in transit is required.
- **RELP version 0 only.** The daemon advertises `relp_version=0` in its open response. Extensions beyond the `syslog` command are not negotiated.
- **Syslog parsing is shared with snooze-syslog.** The `auto` parser detects RFC 3164 vs RFC 5424 per-frame; forcing a parser mode with `parser: rfc3164` can reduce CPU when the fleet is homogeneous.
- **Memory guard.** Frames exceeding `max_frame_bytes` (default 1 MiB) are rejected with a NACK. Increase this value if your syslog messages carry large structured-data payloads.
- **Concurrent sessions.** Each incoming TCP connection is handled in its own goroutine. There is no configurable connection limit; rely on an upstream firewall or load balancer for connection throttling.

