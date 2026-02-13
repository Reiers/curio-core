# Manual Import

Use manual import when snapshot files are already downloaded.

## Command

```bash
curiocore snapshot import --network mainnet --file /path/to/snapshot.car.zst
```

## Expected checks

- file exists
- file is readable
- basic size sanity check

## Notes

- Import progress in alpha is byte-based.
- Set `--keep-snapshot` if you do not want auto-cleanup.
