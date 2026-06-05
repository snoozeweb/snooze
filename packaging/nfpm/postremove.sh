#!/bin/sh
# Post-remove scriptlet shared by the .deb and .rpm packages (run by nfpm).
# Reloads systemd after the unit files are gone. The `snooze` system user and
# the data under /var/lib/snooze are intentionally left in place so a later
# reinstall keeps the operator's database and config.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
