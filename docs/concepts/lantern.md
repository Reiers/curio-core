# Embedded Lantern

Curio Core ships with **Lantern** embedded as a Go library. Lantern is a pure-Go
Filecoin light client that does end-to-end cryptographic verification of the chain:

- **BLS header signatures** — validates miner block signatures via the actor power table.
- **F3 finality certs** — finalised tipsets are gated by GPBFT (F3) certificates pulled
  from peers, not trusted from an upstream RPC.
- **DRAND beacons** — every block's random beacon is verified against DRAND's BLS
  public key.
- **Content-addressed state** — when curio-core asks for `StateMinerInfo` or similar,
  the state tree is fetched as IPLD blocks from peers and the response is reconstructed
  locally — no trust in any single peer.

## Why not just Lotus?

Upstream Curio requires a Lotus full node (~76 GB chain DB, 24-hour initial sync). For a
solo operator running on a single VM, that's an order-of-magnitude infrastructure jump
just to bootstrap.

Lantern reduces that to ~200 MB of local state and a few seconds of bootstrap.

## How does curio-core use Lantern?

Lantern exposes a Lotus-compatible JSON-RPC surface in-process. Curio-core's task
pipelines talk to the same `FullNode` API they would talk to against a real Lotus
node — they just get answers from Lantern instead.

The bits that matter for an SP:

| API surface | Use |
|---|---|
| `Filecoin.ChainHead` / `eth_blockNumber` | Live chain head for proof scheduling + dashboard |
| `Filecoin.StateMinerInfo` | Miner actor metadata for the registered SP |
| `Filecoin.StateGetRandomnessDigestFromBeacon` | The challenge seed input for PDP proofs |
| `Filecoin.MpoolPush` / `Filecoin.EthSendRawTransaction` | Broadcast our proof + settlement txes |
| `eth_call`, `eth_estimateGas`, `eth_getTransactionReceipt` | FEVM contract reads + tx tracking |
| `eth_getBalance`, ERC-20 `balanceOf` | Wallet balance reads for the dashboard |

## FEVM bridge

For Ethereum-style operations that Lantern's local state can't answer (typically
gas estimation for contract calls), Lantern proxies to an upstream Filecoin RPC. By
default that's `https://api.calibration.node.glif.io/rpc/v1` on calibration, or
`https://api.node.glif.io/rpc/v1` on mainnet.

You can override via:

```bash
curio-core run --vm-bridge-rpc https://your-own-rpc.example.com/
```

::: tip
Lantern verifies every block locally; the upstream RPC is **only** used for the small
set of operations Lantern cannot derive (mostly `eth_estimateGas`). It is not a trust
root — a malicious upstream RPC can refuse service but cannot lie about chain state.
:::

## Where is the data?

```
<data-dir>/<network>/headerstore/      # Lantern's header DB
<data-dir>/<network>/blockstore/       # IPLD blocks for state queries
<data-dir>/<network>/peerstore/        # libp2p peer book
```

These are safe to delete if you want a fresh Lantern bootstrap (the next run will
re-discover peers and re-sync headers). Don't delete them while curio-core is running.

## Lantern's own docs

For Lantern internals — bootstrap quorum, F3 cert exchange, beacon resilience — see
the Lantern documentation at <https://github.com/Reiers/lantern> (separate repo, same
operator).
