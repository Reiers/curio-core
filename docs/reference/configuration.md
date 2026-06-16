# Configuration

Curio Core's runtime configuration lives in three places:

1. **CLI flags** — passed to `curio-core run`
2. **`harmony_config` table** — set via the `/setup` wizard or future `curio-core config set` CLI
3. **Environment variables** — see [CLI reference](/reference/cli)

## CLI flags

See the [CLI reference](/reference/cli) for the full list.

## `harmony_config` rows

The state DB holds a single TOML config bundle:

```toml
[Pdp]
MarketAddress = "0x..."
WalletAddress = "0x..."
MinerID       = "f0..."
```

These are required identifiers for the SP. The `/setup` wizard at
`http://127.0.0.1:4711/setup` writes the row; you can also edit it via:

```sql
-- Inspect
SELECT title, substr(config, 1, 300) FROM harmony_config;

-- Update (be careful; restart curio-core after)
UPDATE harmony_config SET config = '<new-toml>' WHERE title = 'base';
```

Changes take effect on the next `curio-core run` restart.

## Network selection

`--network calibration` or `--network mainnet` controls:

- Which contract addresses Curio Core resolves (PDPVerifier, FilecoinPay, USDFC)
- Which DRAND chain Lantern verifies against
- Which Filecoin gateway the FEVM bridge talks to (glif's calibration vs mainnet RPC)
- Which Lantern bootstrap quorum is used

Calibration default RPC: `https://api.calibration.node.glif.io/rpc/v1`
Mainnet default RPC: `https://api.node.glif.io/rpc/v1`

Override either via `--vm-bridge-rpc <url>`.

## Resource budget

The harmonytask engine's resource budget gates which tasks can run concurrently:

```bash
curio-core run --engine-cpu 4 --engine-ram 4GiB
```

Defaults are tuned for a laptop. For a small-VPS production SP, 4 CPU / 4 GiB is fine.
For an SP that handles heavy concurrent client uploads, bump RAM to 8 GiB+.

If a task type's declared `Cost.RAM` exceeds the engine budget, the task sits queued
forever and the dashboard's Tasks page shows it stuck. Curio Core logs a `WARN` line
when this happens (see [upstream PR #1245 follow-on / curio-core#24](https://github.com/Reiers/curio-core/issues/24)).
