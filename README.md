![Snoozeweb Logo](docs/images/logo.png)

# Snooze

Snooze is a clustered log-aggregation and alerting backend. It ingests
events from many input sources (syslog, RELP, SNMP traps, SMTP,
webhooks, Grafana, AlertManager, …), runs them through a pipeline of
aggregations, rules, snoozes and notifications, and routes the
survivors to chat, email, ticketing or arbitrary webhook destinations.

## Install

### Docker (single node, SQLite)

```bash
docker run --name snoozeweb -d -p 5200:5200 \
  -e SNOOZE_DATABASE_TYPE=sqlite \
  -e SNOOZE_DATABASE_PATH=/var/lib/snooze/db.sqlite \
  -v snooze-data:/var/lib/snooze \
  snoozeweb/snooze:latest
```

Web interface: <http://localhost:5200>. Default login: `root:root`
(change it on first connection — the operator CLI can rotate the
root token: `snooze root-token rotate`).

### docker-compose (three backends)

The repo ships a `docker-compose.yaml` with three profiles — pick one:

```bash
docker compose --profile mongo    up    # 3-node Mongo replica set + nginx LB
docker compose --profile postgres up    # single Postgres + single snooze
docker compose --profile sqlite   up    # SQLite on a named volume
```

### Native packages

`.rpm` and `.deb` packages are published on every release; install
them and `systemctl enable --now snooze-server`:

```bash
# RHEL / CentOS / Rocky
sudo dnf install ./snooze-server-<version>.x86_64.rpm

# Debian / Ubuntu
sudo apt install ./snooze-server_<version>_amd64.deb
```

The systemd units, default config, and tmpfiles rules live in
`packaging/systemd/` and `packaging/{rpm,debian}/`.

### Kubernetes / Helm

The chart at `packaging/helm/` deploys `snooze-server` plus any
subset of the input/output binaries. It supports the three database
backends and can provision Postgres via the CloudNativePG operator.
See `packaging/helm/values.yaml` and the JSON schema in
`packaging/helm/values.schema.json`.

```bash
helm install snooze ./packaging/helm \
  --set database.type=sqlite \
  --set persistence.enabled=true
```

## Build from source

```bash
# Toolchain: Go >= 1.25, Task >= 3, Node 18+ (for the React bundle).
task go:build         # builds every cmd/<binary> into ./bin/
task go:test          # unit tests with -race
task go:lint          # golangci-lint
task goreleaser:snapshot   # full multi-arch release tarballs locally
```

The React frontend is built separately:

```bash
cd web && npm ci && npm run build
```

Toolchain: Vite 6 + TypeScript 5.7 + React 19. The build outputs
to `web/dist/`. `snooze-server` serves it via the `-web-dir` flag
(default `/var/lib/snooze/web`, matching the Docker image's copy
path).

`packaging/Dockerfile.golang` builds the React bundle and the Go
binaries in a single multi-stage image; see the `runtime-server`
and `runtime-component` targets.

## Documentation

User documentation: <https://docs.snoozeweb.net> (Docusaurus site built
from `docs/content/`; published to GitHub Pages by `.github/workflows/docs.yml`).

Repo-internal pointers:

* `AGENTS.md` — conventions for human and AI contributors.
* `CHANGELOG.md` — release history.
* `ROADMAP.md` — direction of travel.
* `docs/content/migration/python-to-go.md` — upgrading from 1.x.

### What ships in v2

| Binary               | Role                                                  |
|----------------------|-------------------------------------------------------|
| `snooze-server`      | HTTP API + React web UI + pipeline workers            |
| `snooze`             | Operator CLI (login, root-token, query)               |
| `snooze-syslog`      | Syslog (RFC 3164 / 5424) listener                     |
| `snooze-snmptrap`    | SNMP trap receiver                                    |
| `snooze-relp`        | RELP listener                                         |
| `snooze-smtp`        | SMTP submission listener                              |
| `snooze-googlechat`  | Google Chat output notifier                           |
| `snooze-mattermost`  | Mattermost output notifier                            |
| `snooze-teams`       | Microsoft Teams output notifier                       |
| `snooze-jira`        | Atlassian Jira output notifier                        |
| `snooze-pacemaker`   | Pacemaker cluster integration helper                  |

## License

```
Snooze — log aggregation and alerting.

Copyright 2018-2026 Florian Dematraz <florian.dematraz@snoozeweb.net>
Copyright 2018-2026 Guillaume Ludinard <guillaume.ludi@gmail.com>
Copyright 2020-     Japannext Co., Ltd. <https://www.snoozeweb.co.jp/>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/>.
```
