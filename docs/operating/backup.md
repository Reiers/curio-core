# Backup & restore

A Curio Core SP has two backup-worthy artifacts:

1. **`state.sqlite`** — all metadata: keys, tasks, datasets, rails, message tracking.
   Small (~50–200 MB), high value. Lose this and you lose the SP identity.
2. **The stash directory** — the actual client piece bytes. Large (TBs), can be
   rebuilt only by re-uploading from clients (which fails them on proofs in the
   meantime).

Back them up **separately**: state DB on a frequent schedule, stash on a cheaper
periodic rsync.

## Backing up the state DB

SQLite's online `.backup` command takes a consistent snapshot without blocking writers:

```bash
sqlite3 /var/lib/curio-core/state.sqlite \
  ".backup '/backup/state-$(date +%Y%m%d-%H%M%S).sqlite'"
```

Run from cron every 5–15 minutes. Each backup is a complete standalone DB; you can
restore from any snapshot in isolation.

For a cold backup with full quiesce:

```bash
sudo systemctl stop curio-core
cp /var/lib/curio-core/state.sqlite /backup/state-$(date +%Y%m%d).sqlite
sudo systemctl start curio-core
```

## Backing up the stash

```bash
rsync -a --delete /var/lib/curio-core/stash/ /backup/stash/
```

`--delete` keeps the backup in sync with deletions; if you'd rather keep historical
copies of removed pieces, drop `--delete`.

For off-machine backups, point `rsync` at a remote target (S3 via `rclone`, another
Filecoin SP, etc.).

## Restore

The two pieces must be restored **together** to a consistent point in time:

```bash
sudo systemctl stop curio-core

# Replace state DB
cp /backup/state-20260525-1430.sqlite /var/lib/curio-core/state.sqlite

# Replace stash
rsync -a --delete /backup/stash/ /var/lib/curio-core/stash/

sudo systemctl start curio-core
```

If the state DB references piece UUIDs the stash doesn't contain, `ParkComplete` will
log warnings about missing stash files for those rows. Pieces with intact stash files
keep working; pieces with missing files are effectively lost (the SP cannot prove
them on the next challenge).

## Disaster recovery for the PDP wallet

The PDP wallet is what proves your identity on-chain. **Always** keep a separate
exported copy of the PDP key offline:

```bash
curio-core wallet export --confirm 0x<pdp-address> > ~/.curio-pdp-key.txt
chmod 600 ~/.curio-pdp-key.txt
```

Move that file to encrypted offline storage (USB stick in a safe, password manager
secure note, hardware wallet etc.). If the box dies and you can't recover the SQLite
DB, you can `wallet import` the exported key into a fresh installation and resume the
SP identity — though you lose the proof history and rail state, which then forces
re-discovery on next `PDPv0_PaySettle` cycle.
