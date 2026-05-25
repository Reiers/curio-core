# First-run setup

When you launch `curio-core run` for the first time, a setup wizard is available at
**http://127.0.0.1:14994/setup**.

::: tip Optional
First-run setup is **not mandatory** — the wallet and embedded chain client bootstrap
automatically. The wizard is for configuring optional identifiers your SP needs before
it can advertise to clients (e.g. miner ID for the Service Registry).
:::

## What the wizard collects

The wizard writes a single TOML row into the `harmony_config` table with three required
identifiers:

| Field | Format | Purpose |
|---|---|---|
| Market address | `0x...` | The address that signs deal proposals on-chain |
| Wallet address | `0x...` | The PDP wallet (same as `eth_keys` role=pdp) |
| Miner ID       | `f0...` | The Filecoin miner actor that owns this SP identity |

You can come back to `/setup` later to update them. Changes take effect on the next
`curio-core run` restart.

## What happens automatically

The dashboard is the **primary** UI. You can skip the wizard entirely — every page
loads regardless of whether `harmony_config` has been written. The wizard exists for
operators who want a guided first-time flow.

## Skipping the wizard

If you prefer to set things directly, write the values through the CLI:

```bash
# Not yet implemented — for now use the wizard form or edit harmony_config directly.
# Tracking: see github.com/Reiers/curio-core/issues for "first-run cli"
```

For production deployments, the recommended sequence is:

1. Boot `curio-core run` with `--data-dir` pointing at the persistent location.
2. Hit `/setup` once from a browser via SSH tunnel.
3. Restart so the new identifiers load into the running task pipelines.
4. Verify everything via `/wallets` and `/tasks` on the dashboard.
