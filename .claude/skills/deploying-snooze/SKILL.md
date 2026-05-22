---
name: deploying-snooze
description: Use when shipping code changes from this repo (snoozeweb/snooze 2.0) to the remote host "snooze" — i.e. `snooze.egerie.eu` / `srv-snooze`. Covers building the Go server, the React web bundle, and the auxiliary daemons (syslog/snmptrap/jira/teams), ferrying them over the bastion, installing them, restarting the right systemd unit, and verifying the result. Triggers on phrases like "deploy to snooze", "ship to snooze", "redeploy snooze-server", "push the new web bundle".
---

# Deploying to remote "snooze"

## Overview

Build locally → scp via the bastion → `install` + `systemctl restart` → curl `/healthz` and tail the journal. Always rebuild only what you changed; the auxiliary daemons and the server are independent binaries.

There is **no Ansible / no docker-compose / no deb package** on this host — the prior 1.5.0 docker container was retired in May 2026. Everything is bare-metal binaries under `/usr/bin` driven by systemd, talking to a local mongod (`mongodb://127.0.0.1:27017`).

## Quick reference

| Change touched                                       | Build                                                                                                  | Service to restart           |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | ---------------------------- |
| `cmd/snooze-server/**`, `internal/api`, `internal/plugins`, `internal/db`, `internal/condition`, `internal/modification`, `internal/auth`, `internal/core`, …  | `go build -trimpath -tags 'osusergo,netgo' -o /tmp/snooze-server-new ./cmd/snooze-server` | `snooze-server`              |
| `cmd/snooze-syslog/**`, `internal/components/syslog` | `go build … -o /tmp/snooze-syslog-new ./cmd/snooze-syslog`                                              | `snooze-syslog`              |
| `cmd/snooze-snmptrap/**`, `internal/components/snmptrap` | `go build … -o /tmp/snooze-snmptrap-new ./cmd/snooze-snmptrap`                                       | `snooze-snmptrap`            |
| `cmd/snooze-jira/**`, `internal/components/jira`     | `go build … -o /tmp/snooze-jira-new ./cmd/snooze-jira`                                                  | `snooze-jira`                |
| `cmd/snooze-teams/**`, `internal/components/teams`   | `go build … -o /tmp/snooze-teams-new ./cmd/snooze-teams`                                                | `snooze-teams`               |
| `web/src/**` only                                    | `cd web && npm run build` then `tar -C .. -czf /tmp/web-only.tar.gz web/dist`                          | none (static, snooze-server picks it up)         |
| `internal/pluginimpl/*/metadata.yaml`                | rebuild `snooze-server` (the yaml is `//go:embed`'d)                                                    | `snooze-server`              |

The remote-exec wrapper paths (signature: `<wrapper> <host> "<cmd>"`):

```bash
REMOTE_EXEC=/home/fde/repos/egerie/app/marketplace-ai/plugins/devops-alerting/skills/remote-exec/scripts/remote-exec.sh
REMOTE_SCP=/home/fde/repos/egerie/app/marketplace-ai/plugins/devops-alerting/skills/remote-exec/scripts/remote-scp.sh

# Run a command on the remote
"$REMOTE_EXEC" snooze "<command>"

# Copy a file to the remote (or back; either side can carry the host: prefix)
"$REMOTE_SCP" <local-src> snooze:<remote-dst>
```

`snooze` is the explicit first positional arg in every call — it is **not** baked into the wrapper. The wrapper itself supports any configured host; `snooze` is just the one we're targeting.

## Workflow

### 1. Sanity-check local tree first

```bash
go test -short ./...                   # before any go build
cd web && npx vitest run && cd ..      # only when web/ changed
```

Don't ship red tests.

### 2. Build only what you changed

Build flags are the same for every Go target — pure-Go, trimpaths, static-ish:

```bash
go build -trimpath -tags 'osusergo,netgo' -o /tmp/snooze-server-new ./cmd/snooze-server
```

For the web bundle:

```bash
cd web && npm run build && cd ..
tar -C . -czf /tmp/web-only.tar.gz web/dist
```

`npm run build` runs `tsc -b && vite build`. The output lives in `web/dist/` (~6MB). The server reads it from `/var/lib/snooze/web` at runtime; the dir is owned by `snooze:snooze` and the snooze-server systemd unit has `ReadWritePaths=/var/lib/snooze /var/log/snooze`.

### 3. Ship

Assuming `$REMOTE_EXEC` and `$REMOTE_SCP` are set as shown in the quick reference above (signature: `<wrapper> <host> "<cmd>"` for exec, `<wrapper> <local-src> <host>:<remote-dst>` for scp):

```bash
# Server (or any aux daemon — same shape, swap the binary name)
"$REMOTE_SCP" /tmp/snooze-server-new snooze:/tmp/snooze-server-new

# Web bundle
"$REMOTE_SCP" /tmp/web-only.tar.gz snooze:/tmp/web-only.tar.gz
```

### 4. Install + restart

Server (and analogous for each aux daemon — only the names change):

```bash
"$REMOTE_EXEC" snooze "sudo install -m0755 -o root -g root /tmp/snooze-server-new /usr/bin/snooze-server \
  && sudo systemctl restart snooze-server \
  && sleep 3 && sudo systemctl is-active snooze-server"
```

Web bundle:

```bash
"$REMOTE_EXEC" snooze "tdir=\$(mktemp -d) \
  && sudo tar -xzf /tmp/web-only.tar.gz -C \$tdir \
  && sudo rm -rf /var/lib/snooze/web && sudo mkdir -p /var/lib/snooze/web \
  && sudo cp -a \$tdir/web/dist/. /var/lib/snooze/web/ \
  && sudo chown -R snooze:snooze /var/lib/snooze/web \
  && sudo rm -rf \$tdir"
```

No restart needed for web changes — snooze-server serves the bundle from disk on each request.

### 5. Verify

```bash
"$REMOTE_EXEC" snooze "echo '--- services ---' && \
  sudo systemctl is-active snooze-server snooze-syslog snooze-snmptrap snooze-jira snooze-teams && \
  echo '--- health ---' && curl -sk https://localhost:5200/healthz && echo && \
  echo '--- errors in the last 30s ---' && \
  sudo journalctl -u snooze-server --since '30 seconds ago' --no-pager 2>&1 \
    | grep -E '\"level\":\"(WARN|ERROR)\"' \
    | grep -v 'bootstrap: created initial root user' | head"
```

Expected:

- All five services `active`
- `/healthz` returns `{"status":"ok"}`
- No `ERROR`-level lines

For a smoke test of a specific endpoint, log in as `fdematraz` (admin) or `snooze-bot` (admin service account used by the daemons) and POST a sample alert at `/api/v1/alerts` — that path is **public**, no Bearer token needed.

## Common mistakes / gotchas

1. **Aux daemons use `-c <file>`, snooze-server uses `-config <dir>`.** Don't unify the flag in systemd units. The unit files live at `/lib/systemd/system/snooze-{server,syslog,snmptrap,jira,teams}.service` on the host; templates are under `packaging/systemd/` and `/tmp/snooze-deploy/systemd/` from the original install.

2. **`/usr/bin/snooze-server` is the bare binary, not a wrapper.** Don't try to `dpkg -i` anything; we removed the deb in May 2026. `install -m0755 -o root -g root` is the canonical replace step.

3. **TLS cert at `/etc/snooze/certs/wildcard.{crt,key}`** is the `*.egerie.eu` GlobalSign cert, valid until **2027-04-10**. Renewals must drop the new cert/key in place and HUP snooze-server (it doesn't watch the file — `systemctl restart` is the only way).

4. **rsyslogd owns UDP 514.** snooze-syslog binds **UDP 1514** instead (configured in `/etc/snooze/syslog.yaml`). Don't try to give snooze-syslog 514 unless you stop rsyslog first.

5. **`metadata.yaml` files are embedded via `//go:embed`** at compile time. Editing `internal/pluginimpl/*/metadata.yaml` and just restarting the server **does nothing** — you have to rebuild the binary first.

6. **Mongo holds the source of truth.** `/var/lib/snooze/*.json` (the legacy file-backend leftover) is empty zero-byte files from 2023; ignore them. Local users / rules / records / etc. all live in mongo at `localhost:27017/snooze`.

7. **Public paths today**: `/healthz`, `/readyz`, `/metrics`, `/web/*`, `/api/v1/login/*`, `/api/v1/alerts`, `/api/v1/webhook/{alertmanager,grafana,prometheus,kapacitor,influxdb2}`. Everything else requires a Bearer JWT. Per-route policy is in each plugin's `metadata.yaml::route_defaults.authorization_policy`.

8. **Don't ship while haproxy is failing over.** The `snooze` haproxy frontend on the same host TCP-passes-through `:443` → `127.0.0.1:5200`. A `systemctl restart snooze-server` causes ~3s of 502s on the public URL. Pick a quiet moment or expect a tiny gap.

9. **Bash gotcha with the remote-exec wrapper**: a remote command that contains a bare `--` token at top level (e.g. `mongosh --quiet db -- /tmp/script.js`, or `bash -- foo`) gets eaten by an intermediate bash and the wrapper fails with `bash: --: invalid option`. Short flags (`-m0755`, `-o root`, `-g root`) and long options that are part of the actual argv with values (`--quiet`, `--since='5 minutes ago'`, `--no-pager`) are fine — those don't trip the issue, only a bare `--` separator does. If you need `--` semantics, write the command to a local file, `"$REMOTE_SCP"` it over to `/tmp/<script>`, then exec the path directly: `"$REMOTE_EXEC" snooze "bash /tmp/<script>"`.

10. **Bot password** (`snooze-bot` admin service account) is documented in mongo's `user` collection at boot; the four aux daemons read it from their respective `/etc/snooze/{syslog,snmptrap,jira,teams}.yaml`. Rotating it requires updating both the user doc AND all four yaml files.

## Where things live on the remote

| Path                                         | Purpose                                                |
| -------------------------------------------- | ------------------------------------------------------ |
| `/usr/bin/snooze-server`                     | Main daemon binary                                     |
| `/usr/bin/snooze-{syslog,snmptrap,jira,teams}` | Aux daemon binaries                                   |
| `/usr/bin/snooze`                            | CLI client                                             |
| `/var/lib/snooze/web/`                       | Compiled React bundle                                  |
| `/etc/snooze/server-go/core.yaml`            | Server bootstrap config                                |
| `/etc/snooze/{syslog,snmptrap,jira,teams}.yaml` | Per-daemon configs                                  |
| `/etc/snooze/certs/wildcard.{crt,key}`       | TLS material                                           |
| `/lib/systemd/system/snooze-*.service`       | Systemd units                                          |
| `/var/log/snooze/`                           | Daemon log files (server logs to journald, not here)   |
| `/var/run/snooze/server.socket`              | Admin unix socket — `snooze root-token` reads from it  |
