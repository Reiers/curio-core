# HTTP API reference

Curio Core listens on a single port (default `127.0.0.1:14994`) with four route
prefixes:

| Prefix | Purpose | Auth |
|---|---|---|
| `/pdp/*` | Client-facing PDP API (synapse-sdk speaks this) | Endpoint-dependent (see upstream curio `pdp/` docs) |
| `/piece/*` | Client-facing piece retrieval | None |
| `/admin/*` | Operator diagnostic endpoints | Loopback only by convention |
| `/`, `/wallets`, `/datasets`, ... | Operator dashboard + setup wizard | Loopback only |

::: warning Public exposure
Only `/pdp/*` and `/piece/*` and `/.well-known/*` should be exposed to the public
internet behind a TLS-terminating reverse proxy. **Never** forward `/admin/*` or
dashboard routes to the public side.
:::

## `/pdp/*` — client API

The full PDP HTTP API surface comes from upstream curio's `pdp/` package and matches
what synapse-sdk speaks. Key endpoints:

| Method | Path | Purpose |
|---|---|---|
| POST | `/pdp/data-sets` | Create a new dataset (broadcasts on-chain tx) |
| POST | `/pdp/data-sets/create-and-add` | Combined create + add-pieces |
| GET | `/pdp/data-sets/created/{txHash}` | Poll creation status |
| GET | `/pdp/data-sets/{id}` | Read dataset metadata |
| POST | `/pdp/piece` | Initiate a piece upload |
| POST | `/pdp/piece/uploads` | Mint a streaming-upload session |
| PUT | `/pdp/piece/uploads/{uuid}` | Upload bytes for a session |
| POST | `/pdp/piece/uploads/{uuid}` | Finalize a streaming upload |
| POST | `/pdp/piece/pull` | Request the SP to pull a piece from a public URL |
| GET | `/pdp/piece/{cid}/status` | Query indexing/IPNI status for a piece |
| GET | `/pdp/piece/` | Look up pieces by CID (auth-required, query params) |

See [the upstream pdp/README](https://github.com/filecoin-project/curio/blob/main/pdp/README.md)
for the authoritative shape; curio-core inherits it.

## `/piece/{cid}` — retrieval

```
GET /piece/{pieceCid}
HEAD /piece/{pieceCid}
```

Streams the raw piece bytes for a `parked_pieces.complete=1` row.

| Header | Behavior |
|---|---|
| `Accept-Ranges` | `bytes` (always) |
| `Content-Length` | Exact raw piece size |
| `ETag` | `"<pieceCid>"` (immutable; identical for same CID) |
| `Cache-Control` | `public, max-age=29030400, immutable` (1 year) |
| `Content-Type` | `application/octet-stream` |

Supports HTTP Range (`bytes=0-99` → 206 Partial Content with Content-Range), and
conditional GET (`If-None-Match: "<pieceCid>"` → 304 Not Modified).

## `/admin/*` — operator diagnostic

| Method | Path | Purpose |
|---|---|---|
| POST | `/admin/test-tx` | Build + broadcast an arbitrary FEVM tx through SenderETH |
| GET | `/admin/eth-key` | Return the PDP wallet address |
| GET | `/admin/alerts` | List alert rows |
| GET | `/admin/alerts/counts` | Alert counts by severity + ack status |
| POST | `/admin/alerts/{id}/ack` | Mark an alert acked |

### `POST /admin/test-tx`

Build and dispatch a tx with arbitrary `to` / `value` / `data` through the in-process
SenderETH (which signs with the PDP wallet, assigns nonce + gas, broadcasts).

```http
POST /admin/test-tx
Content-Type: application/json

{
  "to":    "0xabc...",
  "value": "0x16345785d8a0000",   // hex wei, optional (defaults to 1)
  "data":  "0x70a08231..."        // hex calldata, optional (defaults to empty)
}
```

Response:

```json
{ "txHash": "0x...", "from": "0x..." }
```

The `wallet send` CLI is a thin wrapper around this endpoint.

## `/api/*` — dashboard JSON

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/overview` | JSON snapshot of the overview page (chain head, dataset/piece/rail counts, 24h prove counts, scheduler health) |
| POST | `/api/run` | Execute an allowlisted curio-core subcommand (terminal backend) |

### `GET /api/overview`

Returns:

```json
{
  "NowUTC": "2026-05-25T14:09:00Z",
  "Chain": { "HeadEpoch": 3746201, "NetworkID": "calibration" },
  "Stats": {
    "DatasetsActive": 6,
    "DatasetsTerminated": 0,
    "PiecesCompleteCount": 175,
    "PiecesCompleteBytes": 1834862550,
    "RailsActive": 8,
    "RailsTerminated": 0,
    "RecentProveSuccess24": 24,
    "RecentProveFailed24": 0,
    "TasksRunningNow": 1,
    "TasksUnowned": 0
  }
}
```

Cheap to poll; suitable for external monitoring scrapes.

### `POST /api/run`

```http
POST /api/run
Content-Type: application/json

{ "args": ["wallet", "list"] }
```

Returns:

```json
{
  "ok": true,
  "exit_code": 0,
  "stdout": "...",
  "stderr": "",
  "duration": "32ms"
}
```

The argv must match an entry in the allowlist (see [Embedded terminal](/operating/terminal)).
Shell metacharacters are rejected. 30-second hard timeout. stdout/stderr capped at 64 KiB.
