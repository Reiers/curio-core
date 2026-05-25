# Uploading pieces

Curio Core speaks the **synapse-sdk** upload shape: a 2-phase flow where the client
mints a streaming-upload session, then PUTs the bytes against it.

## From the dashboard

The **Upload** page is the easiest way to test the path end-to-end. Pick any file,
click **Start upload**. The dashboard handles both phases and shows a live progress
bar with bytes-uploaded + percent.

Once upload completes, the piece enters the `parked_pieces` pipeline. Within ~30
seconds, `ParkComplete` flips its `complete=1` flag (visible on the Tasks page) and
the piece becomes retrievable via `GET /piece/{cid}`.

## Wire-level flow

```
PHASE 1: mint session

POST /pdp/piece/uploads
Content-Type: application/json
{
  "service":  "public",
  "raw_size": 10485760
}

→ 200 OK
  { "uuid": "8c1f5a2b-..." }

PHASE 2: upload bytes

PUT /pdp/piece/uploads/8c1f5a2b-...
Content-Type: application/octet-stream
<raw bytes>

→ 200 OK
```

The server-side flow:

1. POST creates a row in `pdp_piece_streaming_uploads`, allocates a UUID, returns it.
2. PUT streams the bytes into `<data-dir>/stash/<uuid>.tmp`.
3. After PUT closes, the server computes CommP, writes a `parked_pieces` row +
   `parked_piece_refs` row pointing at the stash file.
4. `ParkComplete` task picks up the row in ~5s polling, flips `complete=1`.

## Retrieval

Once `parked_pieces.complete=1`, the piece is served by:

```
GET /piece/{pieceCid}
```

The handler reads from the same stash file, supports HTTP Range, ETag, conditional
GETs (`If-None-Match` → 304), and `Cache-Control: public, max-age=29030400, immutable`.

See [HTTP API reference](/reference/http-api) for the full endpoint set.

## Pull from URL (client-pulled uploads)

The alternative shape: the client tells the SP to **download** the piece from a public
URL (e.g. an IPFS gateway). This is handled by the `PDPv0_PullPiece` task type:

```
POST /pdp/piece/pull
{
  "service":    "public",
  "piece_cid":  "baga6ea4seaq...",
  "source_url": "https://gateway.example.com/piece/baga6ea4seaq..."
}
```

The SP queues a task, downloads the bytes in the background, verifies CommP, and lands
the piece in `parked_pieces` the same way as a streaming upload.

The pull pipeline has bounded concurrency, restart-safe parking, and HTTP 503 + Retry-After
backpressure — see [upstream PR #1245](https://github.com/filecoin-project/curio/pull/1245)
for the architectural details.
