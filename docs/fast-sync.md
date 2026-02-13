# Fast Sync

Fast sync bootstraps Curio Core from a recent snapshot archive.

## Guided flow (`curio sync`)

1. Select network (`mainnet` / `calibnet`)
2. Select mode (`fast`)
3. Select storage path
4. Download snapshot
5. Verify snapshot
6. Import snapshot
7. Start node skeleton
8. Cleanup snapshot (default: yes)
9. Persist config/status

## Default sources

- Mainnet: `https://forest-archive.chainsafe.dev/latest/mainnet/`
- Calibration: `https://forest-archive.chainsafe.dev/latest/calibnet/`

## Direct commands

```bash
curio snapshot download --network mainnet
curio snapshot import --network mainnet --file <path>
curio snapshot cleanup --network mainnet --all --yes
```

## Progress

- Download progress from `aria2c` output parsing
- Import progress from bytes processed (alpha approximation)
- Status persisted in `~/.curio/status.json`
