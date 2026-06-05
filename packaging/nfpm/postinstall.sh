#!/bin/sh
# Post-install scriptlet shared by the .deb and .rpm packages (run by nfpm).
# Idempotent: creates the unprivileged `snooze` system account, the writable
# state/log directories the systemd units expect, fixes ownership on the
# packaged config and web-UI trees, and reloads systemd so the new units are
# visible. Safe to re-run on upgrades.
set -e

# --- system user/group -----------------------------------------------------
if ! getent group snooze >/dev/null 2>&1; then
    groupadd --system snooze
fi
if ! getent passwd snooze >/dev/null 2>&1; then
    useradd --system --gid snooze \
        --home-dir /var/lib/snooze \
        --shell /usr/sbin/nologin \
        --comment "Snooze server" \
        snooze
fi

# --- writable directories --------------------------------------------------
# /var/lib/snooze holds the default SQLite database and the web bundle;
# /var/log/snooze is a ReadWritePath in the unit. /etc/snooze/server is the
# -config directory (empty by default: the server boots on defaults + SQLite).
for d in /var/lib/snooze /var/log/snooze /etc/snooze/server; do
    mkdir -p "$d"
done
chown -R snooze:snooze /var/lib/snooze /var/log/snooze
chown -R snooze:snooze /etc/snooze/server
chmod 0750 /etc/snooze/server

# --- systemd ---------------------------------------------------------------
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
