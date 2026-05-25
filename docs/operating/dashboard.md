# Dashboard tour

The Curio Core dashboard lives at `http://127.0.0.1:14994/` (default listen address).
It's the single pane of glass for operating the SP — chain head, datasets, rails,
wallets, tasks, alerts, upload, terminal.

::: warning Loopback only
Today the dashboard binds **loopback** with no auth. Reach it via SSH tunnel:

```bash
ssh -L 14994:127.0.0.1:14994 your-sp-host
# then open http://127.0.0.1:14994/ in your browser
```

A production auth layer is on the roadmap. Until then, **do not** expose the
dashboard ports to the public internet.
:::

## The sidebar

Two grouped sections:

- **Monitor** — Overview, Datasets, Rails (USDFC), Tasks, Alerts. Read-only.
- **Operate** — Wallets, Storage, Upload, Terminal. Has actions.

## Overview page

Four headline metrics:

| Card | What it shows |
|---|---|
| **Chain head (epoch)** | Live chain epoch from the embedded Lantern. Ticks every ~30s. |
| **Active datasets** | Non-terminated `pdp_data_sets` rows. |
| **Pieces stored** | Count + total bytes of `parked_pieces` where `complete=1`. |
| **Active rails** | Non-terminated `pdp_payment_rails` rows. |

Below the headline: **24h proof loop** (succeeded / failed counts) and **scheduler
health** (running / queued tasks). Both link through to the **Tasks** page.

The four quick-action cards link to the most common operator pages: Upload, Terminal,
Storage, Wallets.

The page auto-reloads every 30 seconds.

## Wallets page

Per-wallet tFIL + USDFC balances, read live via embedded Lantern (`eth_getBalance`
for native FIL; ERC-20 `balanceOf(0x70a08231)` for USDFC).

Below the table: a **Send a value transfer** form. FIL sends are wired directly via
`/admin/test-tx`. USDFC sends route to the Terminal for now (browser-side calldata
construction is a follow-on).

For wallet creation / import / export, the dashboard links you to the embedded
**Terminal** page with the command pre-filled.

## Datasets page

One row per dataset registered to this SP. Status pill: `healthy` (no failures),
`N consecutive fail` (some proves missed), or `terminated @ <epoch>` (FWSS killed it).

Columns: ID, Status, Next prove epoch, Last challenge epoch, Failures counter,
Init-ready flag.

## Rails page

Header strip: total active rails, total incoming USDFC/epoch rate, settlement cadence.

Table: one row per rail with payer, status, per-rail rate, settled-up-to epoch, and
last `settleRail` tx hash.

## Tasks page

Two tables: **Active** (current `harmony_task` rows, with owner pills) and
**Recent history** (last 50 `harmony_task_history` rows with result + truncated error).

## Alerts page

Mirrors the `alerts` table. Each row: severity pill, ack status, title + optional
message. Currently the alert population is sparse; more triggers ship in upcoming
iterations.

## Storage page

Read-only view of disk usage:

- Pieces stored (count + logical bytes from `parked_pieces`)
- Physical stash directory size (live walk of the on-disk dir)
- Stash dir + data dir paths

## Upload page

Two-phase upload form. Pick a file, click **Start upload**:

1. POSTs `/pdp/piece/uploads` to mint a streaming-upload session.
2. PUTs the file body to `/pdp/piece/uploads/{uuid}` with a live progress bar.

After upload completes, the piece enters the `parked_pieces` pipeline. `ParkComplete`
flips `complete=1` within ~30 seconds (visible on the Tasks page).

## Terminal page

In-browser terminal for **allowlisted** curio-core subcommands:

- `version`
- `wallet list`
- `doctor`
- `sp info`
- `probe`
- `config show`

Up/down arrow scrolls history. Click any chip in the **Quick** row to run it.
Everything else (real `wallet new`, `demo *`, `sp register`, full shell) is rejected.

See [Embedded terminal](/operating/terminal) for the full allowlist + escape model.
