# Storage paths

Curio Core keeps state in **two directories** under the `--data-dir` you pass at boot:

| Directory | What |
|---|---|
| `<data-dir>/state.sqlite` | Single SQLite file with all metadata: tasks, datasets, rails, wallets, alerts, message tracking |
| `<data-dir>/<network>/headerstore/` | Lantern's header DB |
| `<data-dir>/<network>/blockstore/` | IPLD blocks for state queries |
| `<data-dir>/stash/<uuid>.tmp` | Client piece bytes (1:1 with raw size). These are the actual files clients pay you to store. |

## Choosing the data directory

```bash
curio-core run --data-dir /var/lib/curio-core ...
```

Default is `~/.curio-core`. For production, point it at a persistent partition with
enough headroom for the **stash** (the bulk of disk use).

::: tip Stash sizing
- 1 MB piece = 1 MB on disk (raw size, no padding overhead at rest)
- For a small SP, plan ~1 TB stash to start. The proof loop costs only gas; capacity is
  the limit on how much you can offer clients.
:::

## Moving the data dir

Changing paths today **requires a service restart**:

```bash
sudo systemctl stop curio-core
mv /var/lib/curio-core /mnt/bigger-disk/curio-core
# update /etc/systemd/system/curio-core.service: --data-dir /mnt/bigger-disk/curio-core
sudo systemctl daemon-reload
sudo systemctl start curio-core
```

Don't copy — move. The `parked_piece_refs.data_url` rows hold absolute paths into the
stash; if you copy, you end up with two divergent stashes and a broken DB.

Live path changes (rebind without restart) are a planned enhancement.

## Backing up the state DB

The state DB is small (~50–200 MB). The stash is large. Back them up separately.

```bash
# Hot backup of state DB (does not block writers)
sqlite3 /var/lib/curio-core/state.sqlite \
  ".backup '/backup/state-$(date +%Y%m%d).sqlite'"

# Stash backup: rsync to another filesystem
rsync -a --delete /var/lib/curio-core/stash/ /backup/stash/
```

For disaster recovery you need **both**: the state DB knows about the pieces, the
stash holds the bytes. One without the other is useless.

## Where the dashboard shows this

The **Storage** page on the dashboard surfaces:

- Total stored pieces count + logical bytes (from `parked_pieces`)
- Physical disk usage of the stash directory (live walk)
- Current stash + data dir paths (read-only)

If the physical disk usage diverges significantly from the logical bytes, you have
orphan files in the stash — files on disk that no `parked_pieces` row references. The
[`PDPv0_PullPiece`](https://github.com/filecoin-project/curio/pull/1245) refactor adds
backpressure to prevent these orphans on failed uploads; for older state, a
maintenance task can sweep them (tracking issue TBD).
