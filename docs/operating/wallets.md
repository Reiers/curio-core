# Wallet management

Curio Core stores eth_keys (secp256k1 private keys) in the SQLite state DB. Each key
has a **role** that determines which subsystem uses it; the most important is `pdp`,
the wallet that signs all proof + settlement broadcasts.

## CLI

```bash
# List wallets
curio-core wallet list

# Create new (default role: pdp)
curio-core wallet new --role pdp

# Import existing
curio-core wallet import 0x<64-char-hex-private-key> --role pdp

# Export plaintext key (DANGEROUS, requires --confirm)
curio-core wallet export --confirm 0x<address>

# Change role
curio-core wallet role 0x<address> backup

# Delete (refuses the last pdp wallet)
curio-core wallet delete 0x<address>
```

## Sending FIL or USDFC

```bash
# Native FIL
curio-core wallet send --asset fil 0x<to-address> 1.5

# USDFC (ERC-20)
curio-core wallet send --asset usdfc 0x<to-address> 12.34

# Preview the calldata without broadcasting
curio-core wallet send --asset usdfc --dry-run 0x<to-address> 10
```

Amount is decimal in display units. FIL and USDFC both use 18 decimals on EVM rails,
so `1.5` → `1500000000000000000` base units in either case.

The `--daemon` flag overrides the daemon URL (default `http://127.0.0.1:14994`); the
`--network` flag picks the USDFC contract address (calibration vs mainnet).

`wallet send` **requires** the daemon to be running because it routes through
`/admin/test-tx`, which uses the in-process SenderETH (and gives you on-chain tx
tracking via `message_sends_eth` for free).

## From the dashboard

The **Wallets** page on the dashboard shows live balances + a Send form for FIL. USDFC
sends from the WebUI are still CLI-only for now (the page links you to the Terminal
with the command pre-filled).

## Backup

A `state.sqlite` file (under the data dir) holds all the private keys. To back up:

```bash
# Quiesce writes briefly, copy out, restart
sudo systemctl stop curio-core
cp /var/lib/curio-core/state.sqlite ~/curio-core-state-backup-$(date +%Y%m%d).sqlite
sudo systemctl start curio-core
```

Or use the SQLite `.backup` command for a hot backup:

```bash
sqlite3 /var/lib/curio-core/state.sqlite \
  ".backup '/secure-storage/state-$(date +%Y%m%d).sqlite'"
```

For long-term key custody, **export the PDP key** and store it offline:

```bash
curio-core wallet export --confirm 0x<pdp-address> > ~/.curio-pdp-key.txt
chmod 600 ~/.curio-pdp-key.txt
```

Treat that file like any cryptocurrency private key — bearer secret, irrevocable if
leaked. The dashboard's Wallets page lets anyone with loopback access read the same
key out, which is the main reason the dashboard is loopback-only today.

## Multiple wallets

A curio-core SP needs exactly **one** wallet with role=`pdp`. Other wallets you create
with `wallet new --role <name>` are stored alongside but unused by the task pipelines.
You can move funds between them with `wallet send`, but the PDP wallet is the only one
that signs proofs.

The `--role` taxonomy is open-ended; common conventions:

| Role | Purpose |
|---|---|
| `pdp` | The signer for proofs + settlements (one mandatory) |
| `backup` | Cold wallet for sweeping accumulated USDFC out |
| `operator` | Optional separate key for human-driven txes |

Custom roles work; the daemon only treats `pdp` specially.
