---
sidebar_position: 16
---

# Pacemaker (input)

## Overview

**snooze-pacemaker** is a one-shot Pacemaker fence helper. Unlike the other input daemons in this section it is not a long-running server — it is invoked once per fence event by Pacemaker's `stonith-ng` daemon, exactly like any other fence agent (`fence_ipmilan`, `fence_apc`, etc.).

Its sole job is to forward a single fence-event record to `snooze-server` via `pkg/snoozeclient` so the cluster operator has an audit trail every time a node is shot. It does **not** perform any fencing itself; use it alongside a real fence agent in a Pacemaker stonith resource.

Passive actions (`metadata`, `monitor`, `list`, `status`, `validate-all`) exit 0 immediately without touching the network. Only the destructive actions `on`, `off`, and `reboot` generate a Snooze alert.

### What it sends

Each invocation with an active fence action produces one `snoozetypes.Record`:

| Snooze field | Value |
|----|----|
| `source` | constant `"pacemaker"` |
| `host` | name of the node being fenced (from `$nodename`, `$port`, positional arg, or `$HOSTNAME`) |
| `process` | constant `"fence"` |
| `severity` | constant `"critical"` |
| `message` | `$reason` if set, else `"fence <action> requested for node <host>"` |
| `timestamp` | wall-clock time of the invocation |
| `raw.action` | fence action (`"on"`, `"off"`, `"reboot"`) |
| `raw.host` | node name (same as `Record.Host`) |
| `raw.reason` | raw `$reason` value (may be empty) |

Tags `fence` and `cluster` are always attached to the record.

### Exit codes

| Code | Meaning                                                          |
|------|------------------------------------------------------------------|
| 0    | Action handled successfully (alert posted or passive no-op).     |
| 1    | Configuration error (missing `server`, missing node name, etc.). |
| 2    | Unknown action.                                                  |
| 3    | Snooze API error (network failure, authentication error, etc.).  |

Non-zero exits from fence helpers are interpreted by stonith-ng as probe or action failures; configure your other fence agents first so a Snooze API outage does not prevent actual fencing.

## Configuration

`snooze-pacemaker` reads `/etc/snooze/pacemaker.yaml` by default. The config file is optional — all fields can be supplied via environment variables instead (useful in stonith resource configurations). Override the path with `-config` or the `SNOOZE_PACEMAKER_CONFIG` environment variable.

``` yaml
# --- Snooze server (where fence alerts are POSTed) ---
server: "https://snooze.example.com"    # Required (or set SNOOZE_SERVER)
username: "ingest"
password: "change-me"
method: "local"           # auth backend: local | ldap | anonymous
# token: ""               # bearer token (skips username/password)
insecure: false           # disable TLS verification for the Snooze client

# --- Tuning ---
request_timeout: 10s      # per-alert POST timeout (default: 10s)
```

### Field reference

| Key               | Env-var override Meaning                              |
|-------------------|-------------------------------------------------------|
| `server`          | `SNOOZE_SERVER` Snooze base URL. **Required.**        |
| `username`        | `SNOOZE_USERNAME` Login username.                     |
| `password`        | `SNOOZE_PASSWORD` Login password.                     |
| `method`          | `SNOOZE_METHOD` Auth backend; defaults to `local`.    |
| `token`           | `SNOOZE_TOKEN` Bearer token; short-circuits login.    |
| `insecure`        | `SNOOZE_INSECURE` Skip TLS verification (`true`/`1`). |
| `request_timeout` | — Per-request timeout; defaults to `10s`.             |

Additionally, `SNOOZE_TOKEN_CACHE_FILE` may be set to point at a file the helper uses to cache the bearer token between invocations, avoiding a fresh login on every fence event.

:::note

Environment variables win over YAML when both are present. Pacemaker resource parameters and systemd `Environment=` directives are the typical way to supply credentials without writing them to a shared config file.

:::

There is no systemd unit for snooze-pacemaker — it is invoked by stonith-ng as a transient child process, not as a persistent service.

## Usage

### Registering the fence helper in Pacemaker

Install the binary at `/usr/bin/snooze-pacemaker` (or wherever your distro places fence agents). Then verify that Pacemaker can see its metadata:

``` console
$ /usr/bin/snooze-pacemaker metadata
```

Create a stonith resource that is used alongside your real fence agent — the Snooze helper is a notification side-car, not a replacement:

``` console
$ pcs stonith create fence-snooze-notify fence_snooze_pacemaker \
    pcmk_host_list="node-01 node-02 node-03" \
    pcmk_off_action="off" \
    pcmk_reboot_action="reboot"
```

Or in raw CIB XML:

``` xml
<primitive id="fence-snooze-notify" class="stonith"
           type="snooze-pacemaker">
  <instance_attributes id="fence-snooze-notify-attrs">
    <nvpair name="pcmk_host_list" value="node-01 node-02 node-03"/>
  </instance_attributes>
  <meta_attributes id="fence-snooze-notify-meta">
    <!-- Run after the real fence agent so notification doesn't block fencing -->
    <nvpair name="priority" value="0"/>
  </meta_attributes>
</primitive>
```

### Supplying credentials through the resource

Pacemaker resource parameters are passed as environment variables and (for stonith) also via stdin `key=value` lines. You can supply Snooze credentials directly in the resource definition:

``` console
$ pcs stonith create fence-snooze-notify snooze-pacemaker \
    SNOOZE_SERVER="https://snooze.example.com" \
    SNOOZE_USERNAME="ingest" \
    SNOOZE_PASSWORD="change-me"
```

Alternatively, place credentials in `/etc/snooze/pacemaker.yaml` and rely on the default config path — stonith-ng runs helpers as root so the file is always readable.

## Testing / verifying

Run the helper directly to verify credentials and connectivity before wiring it into a cluster:

``` console
# Passive probe — exits 0, prints metadata XML
$ snooze-pacemaker -config /etc/snooze/pacemaker.yaml metadata

# Dry-run a fence-off event for node-01
$ SNOOZE_SERVER=https://snooze.example.com \
  SNOOZE_TOKEN=<bearer> \
  snooze-pacemaker off node-01

# Check the resulting record in the Snooze API
$ curl -sS -H 'Authorization: Bearer <bearer>' \
    'https://snooze.example.com/api/v1/record' \
    | jq '.[] | select(.source=="pacemaker") | {host,message,severity,raw}'
```

You can also simulate the stdin-driven invocation that stonith-ng uses:

``` console
$ printf 'action=off\nnodename=node-01\nreason=node unresponsive\n' \
    | snooze-pacemaker -config /etc/snooze/pacemaker.yaml
```

## Notes & limitations

- **Notification only — performs no fencing.** The helper posts an alert and exits. It must be used alongside a real fence agent. A non-zero exit from this helper does **not** prevent `stonith-ng` from proceeding with actual fencing if configured correctly (use `priority` or `pcmk_off_action` ordering to ensure the real agent fires first).
- **Token cache across invocations.** By default each invocation performs a fresh login if no bearer token is configured. Set `SNOOZE_TOKEN_CACHE_FILE` to a writable path (e.g. `/run/snooze/pacemaker.token`) to persist the token between calls and avoid the login round-trip on hot fencing paths.
- **Timeout is intentionally short.** The default `request_timeout` is 10 s. Fence helpers run on hot cluster-management paths; a long-hanging HTTP call would delay the stonith-ng acknowledgement. Increase it only if your network latency to the Snooze server justifies it.
- **No systemd unit.** The helper is a one-shot process managed by Pacemaker's `stonith-ng`. There is no persistent service to manage or restart.
- **Stdin parameter parsing.** The helper reads at most 1 MiB from stdin. Lines with the format `key=value` are folded into the environment; existing environment variables take precedence (standard fence-agent convention).
- **Unknown actions return exit code 2.** Any action not in the supported set (`on`, `off`, `reboot`, `metadata`, `monitor`, `list`, `status`, `validate-all`) produces exit code 2. stonith-ng treats this as an error.

