#!/bin/sh
# postinstall: create the curio-core system user + data dir, reload systemd.
# Idempotent — safe on upgrade.
set -e

CC_USER=curio-core
CC_HOME=/var/lib/curio-core

# Create a locked system user/group if missing.
if ! getent group "$CC_USER" >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system "$CC_USER"
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup --system "$CC_USER"
    fi
fi
if ! getent passwd "$CC_USER" >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid "$CC_USER" --home-dir "$CC_HOME" \
            --no-create-home --shell /usr/sbin/nologin "$CC_USER"
    elif command -v adduser >/dev/null 2>&1; then
        adduser --system --ingroup "$CC_USER" --home "$CC_HOME" \
            --no-create-home --shell /usr/sbin/nologin "$CC_USER"
    fi
fi

# Data dir, owned by the service user.
mkdir -p "$CC_HOME"
chown -R "$CC_USER":"$CC_USER" "$CC_HOME"
chmod 0750 "$CC_HOME"

# Reload systemd so the unit is visible. Do NOT auto-start: a fresh
# install needs `curio-core` first-run setup (wallet bootstrap + funding)
# before the SP is useful. We only enable+start on explicit operator action.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

echo "curio-core installed."
echo "  1) start:  sudo systemctl enable --now curio-core"
echo "  2) setup:  open http://127.0.0.1:4711/setup"
echo "  3) docs:   https://curio-core-docs.pages.dev/"

exit 0
