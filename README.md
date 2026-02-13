# Curio Core (Alpha)

Curio Core is a private alpha sync-node project informed by deep architectural analysis of Lotus, Venus, and Forest.

## Alpha scope

This alpha focuses on bootstrap UX and sync-node scaffolding:
- guided setup flow (`curiocore sync`)
- fast sync from snapshots
- manual snapshot import
- status tracking with stage/progress output
- node process skeleton + datastore layout

## Fast sync

Fast sync bootstraps from a recent snapshot, imports it, and then starts syncing from that state.

Default snapshot sources:
- Mainnet: `https://forest-archive.chainsafe.dev/latest/mainnet/`
- Calibration: `https://forest-archive.chainsafe.dev/latest/calibnet/`

## Networks

- `mainnet`
- `calibnet`

## Quickstart

```bash
curiocore init
curiocore sync
```

## Advanced

Download only:
```bash
curiocore snapshot download --network mainnet
```

Import only:
```bash
curiocore snapshot import --network mainnet --file ~/.curiocore/snapshots/mainnet/mainnet-latest.car.zst
```

Cleanup only:
```bash
curiocore snapshot cleanup --network mainnet --all --yes
```

Status:
```bash
curiocore status
curiocore status --watch
```

## Disk paths

Default home: `~/.curiocore/` (override with `CURIOCORE_HOME`)

- config: `~/.curiocore/config.json`
- status: `~/.curiocore/status.json`
- snapshots: `~/.curiocore/snapshots/<network>/`
- data: `~/.curiocore/data/<network>/`

## Verify success

`curiocore status` should move through stages:
- `downloading`
- `verifying`
- `importing`
- `syncing`
- `complete`

## Troubleshooting

- Ensure `aria2c` is installed and in PATH.
- Ensure snapshot URL is reachable.
- Use `--snapshot-url` override if mirror changes.
- Ensure write permissions on `~/.curiocore`.

## Roadmap

See:
- `docs/architecture.md`
- `docs/roadmap.md`
- `docs/fast-sync.md`
- `docs/manual-import.md`
