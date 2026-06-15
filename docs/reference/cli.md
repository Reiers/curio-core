# CLI reference

```bash
curio-core [subcommand] [flags] [args...]
```

## Subcommands

### `run`

Start the daemon: embedded Lantern + harmonytask engine + dashboard + HTTP API.

```bash
curio-core run [flags]
```

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--data-dir` | `~/.curio-core` | Persistent state directory (SQLite + Lantern + stash) |
| `--network` | (from build) | `calibration` or `mainnet` |
| `--listen` | `127.0.0.1:14994` | Deprecated alias for `--admin-listen` |
| `--admin-listen` | (falls back to `--listen`) | Loopback admin/UI surface: dashboard, `/setup`, `/admin/*` |
| `--public-listen` | (empty = single-port) | Public surface: `/pdp/*` + `/piece/*`. Empty keeps everything on the admin port |
| `--public-tls-domain` | (empty) | Domain for baked-in `autocert` TLS on the public port. Empty serves plaintext (dev) and refuses `:443` |
| `--acme-http-listen` | `:80` | ACME HTTP-01 challenge + HTTP→HTTPS redirect bind (only with `--public-tls-domain`) |
| `--acme-directory-url` | (empty) | Override ACME directory (LetsEncrypt staging / tests) |
| `--vm-bridge-rpc` | per-network default | Upstream Filecoin RPC for FEVM operations |
| `--vm-bridge-rpc-disable` | false | Disable the FEVM bridge entirely (offline-only mode) |
| `--engine-cpu` | 4 | harmonytask CPU budget |
| `--engine-ram` | 4GiB | harmonytask RAM budget |

### `wallet`

Manage operator eth_keys.

```bash
curio-core wallet list
curio-core wallet new [--role pdp|backup|operator|<custom>]
curio-core wallet import 0x<64hex> [--role <r>]
curio-core wallet export --confirm 0x<addr>
curio-core wallet role 0x<addr> <new-role>
curio-core wallet delete [--yes] 0x<addr>
curio-core wallet send [--asset fil|usdfc] [--daemon URL] [--network N] [--dry-run] 0x<to> <amount>
```

See [Wallet management](/operating/wallets) for the full guide.

### `sp`

Service Provider operations (Filecoin Service Registry).

```bash
curio-core sp info                  # Read this SP's registration
curio-core sp register --submit     # Register on the Service Registry (costs 5 FIL)
```

### `doctor`

Read-only health + DB ↔ chain reconciliation report. Safe to run anytime.

```bash
curio-core doctor [--data-dir <path>]
```

Reports:

- DB schema version + applied migrations
- Lantern head epoch
- Recent `message_sends_eth` rows + their on-chain status
- Wallets with tFIL + USDFC balances
- Open datasets + their proof status
- Rails + their last settlement attempt

### `probe`

Smoke-test the embedded Lantern's anchoring against the upstream gateway.

```bash
curio-core probe [--network calibration|mainnet]
```

### `config`

Headless config inspection.

```bash
curio-core config show                   # Print all harmony_config rows
curio-core config get <section>.<key>    # Read a single value (planned)
```

### `demo`

End-to-end demo flows for testing the synapse-sdk integration shape.

```bash
curio-core demo prepare-client-payments [flags]
curio-core demo create-dataset [flags]
```

These broadcast on-chain txes — use only on calibration with a funded test wallet.

### `version` / `help`

```bash
curio-core version
# curio-core 0.0.1-prealpha

curio-core help
# Prints the usage summary above.
```

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `CURIO_CORE_PAYMENTS_MIN_SETTLE_EPOCHS` | (unset) | Override the rail-settlement grace window for testing. Production should leave this unset. |
| `GOLOG_LOG_LEVEL` | `info` | Log verbosity (`debug`, `info`, `warn`, `error`) |

All other configuration lives in `harmony_config` (via the `/setup` wizard) and the
`run` flags above.
