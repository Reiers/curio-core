# Upgrading

Curio Core is pre-alpha — expect frequent binary swaps as we ship.

## In-place upgrade

```bash
# Download the new binary
curl -L https://github.com/Reiers/curio-core/releases/latest/download/curio-core-linux-amd64 \
  -o /tmp/curio-core-new
chmod +x /tmp/curio-core-new

# Stop, swap, restart
sudo systemctl stop curio-core
sudo cp /usr/local/bin/curio-core /usr/local/bin/curio-core.previous   # keep a rollback
sudo mv /tmp/curio-core-new /usr/local/bin/curio-core
sudo systemctl start curio-core

# Verify
curio-core version
sudo journalctl -u curio-core --since "1 minute ago" --no-pager | head -30
```

The state DB carries forward unchanged — only the binary swaps.

## Schema migrations

Each release that touches the schema includes new migration files under
`internal/harmonysqlite/schema-curio-core/NNNN_<name>.sql`. The daemon applies them
forward-only on startup and records each in `harmony_schema_migrations`.

You don't run anything manually — `curio-core run` handles it. If a migration fails on
startup, the daemon refuses to come up and logs the offending SQL.

## Rolling back

Migrations are **forward-only**. Curio Core does not ship `downgrade/` SQL like
upstream does, because:

1. Single-node deployments have a clear "snapshot-and-roll-forward" model: take a
   `state.sqlite` backup before upgrading, and if the new release misbehaves, restore
   from backup + revert to the previous binary.
2. We're pre-alpha; the cost of supporting downgrade is high and the audience is
   small.

So the rollback flow is:

```bash
# Stop the service
sudo systemctl stop curio-core

# Restore your pre-upgrade state DB backup
cp /backup/state-pre-upgrade.sqlite /var/lib/curio-core/state.sqlite

# Restore the previous binary
sudo mv /usr/local/bin/curio-core.previous /usr/local/bin/curio-core

sudo systemctl start curio-core
```

Once we go beta and the audience grows, expect proper downgrade support.

## Lantern upgrades

Lantern is bumped via curio-core's `go.mod`; new Lantern versions ship inside the
curio-core binary. You don't manage Lantern separately.

If Lantern's protocol version or DRAND key changes (rare), the upgrade notes will
flag it. Today the upgrade story is: download new curio-core, restart, done.

## Calibration vs mainnet

Calibration upgrades typically lead mainnet by 1–2 weeks. Watch
[curio-core#10](https://github.com/Reiers/curio-core/issues/10) for the per-release
calibration→mainnet promotion checklist.
