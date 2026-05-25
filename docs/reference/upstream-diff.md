# Differences vs upstream Curio

Curio Core consumes upstream [filecoin-project/curio](https://github.com/filecoin-project/curio)
as a Go module and adds three things:

1. A SQLite-backed `harmonyquery.DBInterface` implementation
2. Curio-core-shape replacements for cluster-aware components (`localpiecepark`,
   `diskstash`)
3. A new operator dashboard + USDFC settlement task + a few QoL CLI tools

It does **not** fork upstream Go files. The integration is interface-driven so we
follow upstream's releases without merge conflicts.

## What runs vs what's stripped

| Upstream component | Curio Core | Why |
|---|---|---|
| Yugabyte-based `harmonydb` | SQLite via `harmonyquery.DBInterface` | One binary, one file |
| Lotus full node | Embedded Lantern | No 76 GB chain DB |
| All sealing tasks (`SDR`, `TreeRC`, `PoRep`, `CommitBatch`, `MoveStorage`, etc.) | **Stripped** | Hot-storage doesn't seal |
| WindowPoSt / WinningPoSt | **Stripped** | Sealed-storage proofs only |
| `Curio Markets` (Boost) | **Stripped** | PDP is the new market shape |
| MK20 deals | **Stripped** | Not needed for PDP-only SPs |
| `lib/paths` cluster storage | `internal/localpiecepark` + `internal/diskstash` | Single-node shape |
| `cmd/curio` CLI | `cmd/curio-core` | Reduced surface |
| Curio WebUI (vendored `web/` tree) | New `internal/dashboard` | Curio Core branding + reduced scope |
| `lib/cachedreader` shared piece reader | Stripped (single-node = no cache needed) | OS page cache is enough |
| IPNI advertiser sidecar | Not yet wired | Tracking in [curio-core#42](https://github.com/Reiers/curio-core/issues/42) |
| `tasks/pay/SettleLockupPeriod` | Replaced with `internal/payments.PDPv0_PaySettle` | Same idea, simpler shape for single-payee |

## What's kept verbatim from upstream

- The harmonytask scheduler engine itself (`harmony/harmonytask/`).
- The PDPv0 task surface (`tasks/pdpv0/*`): `Prove`, `InitPP`, `NextPP`, `PullPiece`,
  `Notify`, `ChainSync`, `SaveCache`, `DelDataSet`, `TermFWSS`, watchers.
- The `pdp` HTTP API package serving `/pdp/*` (uploads, retrievals, dataset CRUD).
- The FEVM contract bindings in `pdp/contract/` and `lib/filecoinpayment/`.
- SenderETH and SendTaskETH for on-chain message dispatch.

When upstream ships a PDP fix, we typically rebase our `db-seam-refactor` branch and
the change flows through. Recent example: [PR #1245 (PullPiece refactor)](https://github.com/filecoin-project/curio/pull/1245)
was adopted in a few hours.

## When to use upstream Curio instead

Use upstream Curio if you need any of:

- Sealing (proof-of-replication for cold storage)
- WindowPoSt / WinningPoSt
- Multi-node cluster operation
- Boost / MK20 deal markets
- IPFS HTTP gateway
- Production scale (today; pre-alpha is the limit on curio-core)

Curio Core is for the single-node hot-storage shape. Upstream Curio is the right
choice for everything else.
