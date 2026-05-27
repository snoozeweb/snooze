---
sidebar_position: 1
---

# Installation on RHEL/Debian

:::note

Snooze 2.0 is a Go binary distributed via GoReleaser-built `.deb` / `.rpm` packages. The system Python install is no longer a prerequisite.

:::

## Installation on CentOS / RHEL / Rocky / Alma

``` console
$ wget https://rpm.snoozeweb.net -O snooze-server-latest.rpm
$ sudo dnf localinstall snooze-server-latest.rpm
$ sudo systemctl start snooze-server
```

## Installation on Ubuntu / Debian

``` console
$ wget https://deb.snoozeweb.net -O snooze-server-latest.deb
$ sudo apt install ./snooze-server-latest.deb
$ sudo systemctl start snooze-server
```

The package drops:

- `/usr/bin/snooze-server` — the daemon.
- `/usr/bin/snooze` — the CLI client.
- `/etc/snooze/server-go/*.yaml` — empty starter config; the loader also accepts the legacy `/etc/snooze/server/*.yaml` directory.
- `/lib/systemd/system/snooze-server.service` — unit file.
- `/var/lib/snooze/` — SQLite working directory (also Postgres / Mongo backup location).

:::info

By default Snooze uses SQLite as a single embedded file under `/var/lib/snooze`. This is convenient for testing and small single-node deployments; for production with more than a handful of alerts per second, switch to MongoDB (multi-writer) or PostgreSQL (single-writer with CloudNativePG). See [Core configuration](../configuration/core.md) for the database knobs.

:::

## Web interface

``` console
http://localhost:5200
```

The bootstrap root password is logged once on first start. Look for it in `journalctl -u snooze-server`.

