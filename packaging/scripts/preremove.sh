#!/bin/sh
# preremove: stop + disable the service before files are removed.
# On upgrade the package manager handles restart; only stop on real removal.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop curio-core >/dev/null 2>&1 || true
    systemctl disable curio-core >/dev/null 2>&1 || true
fi

# Intentionally DO NOT delete /var/lib/curio-core: it holds the operator's
# wallet keys, SQLite state, and Lantern header store. Removing the package
# must never destroy chain identity or funds. Document manual purge in the
# uninstall docs instead.

exit 0
