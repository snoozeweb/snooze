# Snooze systemd units

Service unit files for the Snooze Go daemons.

## Install

```sh
cp packaging/systemd/*.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now snooze-server
```

Enable additional ingestors / notifiers as needed:

```sh
systemctl enable --now snooze-syslog snooze-snmptrap snooze-smtp \
                       snooze-relp snooze-googlechat \
                       snooze-mattermost snooze-teams snooze-jira
```

## Requirements

- The `snooze` system user and group must exist. The Debian and RPM
  packages create them in their post-install scripts; if you install the
  units by hand, create the account first:

  ```sh
  useradd --system --home-dir /var/lib/snooze --shell /usr/sbin/nologin snooze
  ```

- Writable directories used by the units:
  - `/var/lib/snooze` — state
  - `/var/log/snooze` — logs (when not using journal-only)
  - `/run/snooze` — admin Unix socket (created automatically by
    `RuntimeDirectory=` on the server unit)

- Configuration is read from `/etc/snooze/<name>.yaml`.

## Database

`snooze-server.service` does not declare a hard dependency on any
specific database service because Snooze supports MongoDB, PostgreSQL
and a file backend. If you co-locate a DB, drop in an override:

```sh
systemctl edit snooze-server
```

and add e.g.:

```ini
[Unit]
Wants=mongodb.service
After=mongodb.service
```

## Privileged ports

`snooze-syslog`, `snooze-snmptrap` and `snooze-smtp` may bind below
port 1024 (514/udp, 162/udp, 25/tcp). The units grant
`CAP_NET_BIND_SERVICE` via `AmbientCapabilities=` so the daemons can
bind those ports while still running as the unprivileged `snooze` user.

## Verify

```sh
systemd-analyze verify packaging/systemd/snooze-*.service
```
