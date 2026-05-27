---
sidebar_position: 13
---

# SNMP Traps (input)

## Overview

**snooze-snmptrap** is a standalone daemon that receives SNMP v1, v2c and v3 trap PDUs and converts each one into a Snooze alert. It is a separate process that owns a UDP listener (by default on the well-known port 162) and forwards parsed trap records to `snooze-server` via `pkg/snoozeclient`.

The daemon uses [gosnmp](https://github.com/gosnmp/gosnmp) for trap decoding and optionally [gosmi](https://github.com/sleepinggenius2/gosmi) for MIB-based OID resolution. When MIB loading is not configured, varbind keys are stored as sanitised dotted OIDs (dots replaced with underscores).

### What it ingests

Each received trap PDU becomes one `snoozetypes.Record` with the following field mapping:

| Snooze field | Trap source |
|----|----|
| `source` | constant `"snmptrap"` |
| `host` | sending IP address (dotted decimal) |
| `process` | last component of the trap OID (v2c/v3), or the enterprise OID (v1), or `"snmptrap"` when neither is present |
| `severity` | heuristic from varbind labels containing `"severity"` or `"priority"`; defaults to `"warning"` |
| `message` | `key=value` rendering of all varbinds, sorted by key |
| `timestamp` | wall-clock time when the trap was received |
| `raw` | full OID→value map of all varbinds, plus `snmp_version` (`"1"`, `"2c"`, or `"3"`) |

### Severity mapping

Severity is extracted heuristically. The daemon scans all resolved varbind names for the substrings `"severity"` or `"priority"` (case-insensitive). The first match is normalised to the Snooze vocabulary:

| Incoming value (case-insensitive) | Snooze severity    |
|-----------------------------------|--------------------|
| `crit`, `critical`, `fatal`,      | `emerg` `critical` |
| `err`, `error`                    | `err`              |
| `warn`, `warning`                 | `warning`          |
| `notice`                          | `notice`           |
| `info`, `informational`           | `info`             |
| `debug`                           | `debug`            |
| `ok`, `clear`, `normal`           | `ok`               |

When no severity varbind is present the severity defaults to `"warning"`. Without MIB resolution (pure dotted-OID keys) the severity varbind heuristic will never fire; set up `mib_dirs` / `mib_list` if you need it.

## Configuration

`snooze-snmptrap` reads `/etc/snooze/snmptrap.yaml` by default; override the path with `-c`.

``` yaml
# --- Snooze server (where alerts are POSTed) ---
server: "https://snooze.example.com"    # Required
username: "ingest"
password: "change-me"
method: "local"           # auth backend: local | ldap | anonymous
insecure: false           # disable TLS verification for the Snooze client

# --- SNMP listener ---
listen: "0.0.0.0:162"    # UDP bind address (default: 0.0.0.0:162)
community: "public"       # v1/v2c community string; "*" accepts any community

# --- SNMPv3 (optional; omit the v3 block entirely for v1/v2c-only) ---
# v3:
#   user: "trapuser"
#   auth_proto: "sha256"     # none | md5 | sha | sha224 | sha256 | sha384 | sha512
#   auth_passphrase: "authpass"
#   priv_proto: "aes"        # none | des | aes | aes192 | aes256
#   priv_passphrase: "privpass"

# --- MIB resolution (optional) ---
# mib_dirs:
#   - /usr/share/snmp/mibs
# mib_list:
#   - SNMPv2-MIB
#   - IF-MIB

# --- Tuning ---
timeout: 30s              # per-alert POST timeout (default: 30s)
```

### Field reference

| Key | Meaning |
|----|----|
| `server` | Snooze base URL. **Required.** |
| `username` / `password` | Credentials for the v1 `/login` endpoint. |
| `method` | Auth backend; defaults to `local`. |
| `insecure` | Skip TLS verification for the Snooze client. |
| `listen` | UDP bind address; defaults to `0.0.0.0:162`. |
| `community` | v1/v2c community string; defaults to `"public"`. Use `"*"` to accept any community. |
| `v3.user` | SNMPv3 USM username. **Required when \`\`v3:\`\` is set.** |
| `v3.auth_proto` | Authentication protocol: `none` \| `md5` \| `sha` \| `sha224` \| `sha256` \| `sha384` \| `sha512`. |
| `v3.auth_passphrase` | Authentication passphrase. |
| `v3.priv_proto` | Privacy (encryption) protocol: `none` \| `des` \| `aes` \| `aes192` \| `aes256`. |
| `v3.priv_passphrase` | Privacy passphrase. |
| `mib_dirs` | Filesystem paths searched for MIB module files. |
| `mib_list` | MIB module names to load (case-sensitive, e.g. `SNMPv2-MIB`). MIB resolution is skipped when this list is empty. |
| `timeout` | Per-request timeout; defaults to `30s`. |

### systemd unit

``` ini
[Unit]
Description=Snooze SNMP trap ingestion daemon
Documentation=https://github.com/snoozeweb/snooze
After=network-online.target snooze-server.service
Wants=network-online.target

[Service]
Type=simple
User=snooze
Group=snooze
ExecStart=/usr/bin/snooze-snmptrap -c /etc/snooze/snmptrap.yaml
Restart=on-failure
RestartSec=5s

ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ReadWritePaths=/var/lib/snooze /var/log/snooze
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

`CAP_NET_BIND_SERVICE` is required to bind the privileged UDP port 162 without running as root.

## Usage

### net-snmp / snmptrap

Send a test v2c trap from another host (requires `net-snmp`):

``` console
$ snmptrap -v 2c -c public snooze-host:162 '' \
    SNMPv2-MIB::coldStart.0 \
    SNMPv2-MIB::sysDescr.0 s "test trap from $(hostname)"
```

Configure `snmptrapd` on a relay host to forward to snooze-snmptrap — useful when you have an existing trap collector and want to tee copies to Snooze:

``` text
# Forward every trap to snooze-snmptrap
traphandle default snmptrap -v 2c -c public snooze-host:162
```

### Network devices

On Cisco IOS / NX-OS:

    snmp-server host snooze-host traps version 2c public

On a Linux node (net-snmp permanent configuration):

    # /etc/snmp/snmp.conf
    trapsink snooze-host public

## Testing / verifying

Send a v2c test trap and verify it arrives in Snooze:

``` console
# Send the trap
$ snmptrap -v 2c -c public snooze-host:162 '' \
    SNMPv2-MIB::coldStart.0

# Poll the Snooze API for the resulting record
$ curl -sS -H 'Authorization: Bearer <token>' \
    'https://snooze.example.com/api/v1/record' \
    | jq '.[] | select(.source=="snmptrap") | {host,process,message}'
```

For a v3 test:

``` console
$ snmptrap -v 3 -u trapuser -l authPriv \
    -a SHA -A authpass -x AES -X privpass \
    snooze-host:162 '' SNMPv2-MIB::coldStart.0
```

## Notes & limitations

- **UDP only.** SNMP traps are always sent over UDP; TCP inform-requests are not supported in this version.
- **Back-pressure / queue.** Received traps are queued in an in-memory buffer (1024 slots by default) before being forwarded to Snooze. If the Snooze server is unreachable the queue fills and excess traps are dropped with a log warning — not retried. Size the alert-pipeline to keep up with your trap rate.
- **MIB resolution is best-effort.** Loading MIBs at startup may fail silently (warning logged); the daemon falls back to raw dotted-OID keys and continues receiving traps. The severity heuristic requires resolved names and will produce `"warning"` for everything when MIBs are not loaded.
- **Community enforcement.** For v1/v2c traps, packets whose community string does not match `community` are silently dropped. Set `community: "*"` to accept traps from mixed fleets.
- **SNMPv3 security model.** Only USM (User Security Model) is supported. Community-string authentication is not applicable to v3 traffic.
- **No counter/gauge normalisation.** Counter32, Counter64, Gauge32, TimeTicks and similar numeric BER types are forwarded as raw integers. Divide by 100 in a Snooze transform rule when you need wall-clock-second values from TimeTicks.

