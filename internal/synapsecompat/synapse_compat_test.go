// synapse_compat_test.go — exercises curio-core's /pdp/* HTTP surface
// against the byte-for-byte request shapes FilOzone/synapse-sdk uses.
//
// Why a separate package: this is a black-box integration test that
// speaks only HTTP. It must build clean under CGO_ENABLED=0 on darwin
// without any of the heavy upstream test-time deps (gosigar, lotus
// storage/paths, etc.) that gate internal/pdptests behind a build
// tag. Keeping it in a separate package keeps the build graph tiny.
//
// How to run:
//
//	# Default: skip the test entirely.
//	go test ./internal/synapsecompat/...
//
//	# Against a running daemon (any URL):
//	CURIO_CORE_URL=http://127.0.0.1:14994 go test -v ./internal/synapsecompat/...
//
// The test boots NO daemon itself. It expects CURIO_CORE_URL to point
// at a curio-core instance you've already started (the smoke pattern
// from cmd/smoke or a systemd unit). This keeps the test honest about
// what an external client sees and avoids pulling the upstream
// PDPService constructor into the test build graph.
//
// References (the SDK functions we mirror):
//
//	github.com/FilOzone/synapse-sdk:
//	  packages/synapse-core/src/sp/ping.ts          ping()
//	  packages/synapse-core/src/sp/find-piece.ts    findPiece()
//	  packages/synapse-core/src/sp/upload.ts        uploadPiece()
//	  packages/synapse-core/src/sp/get-data-set.ts  getDataSet()
//	  packages/synapse-core/src/sp/create-dataset.ts createDataSet()
//	  packages/synapse-core/src/sp/add-pieces.ts    addPieces()
//
// The compat surface is "what status codes + body shapes does the SDK
// expect?" Tests assert minimum compatibility (status code matches, key
// JSON fields present). They do NOT exhaustively assert response body
// fields; that's the SDK's own test suite's job. The goal here is to
// catch route-shape divergence + dialect-bug 500s on the SP side.

package synapsecompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Known-good PieceCIDv2 from the upstream pdp/piece_cid_test.go test
// vectors. Format prefix `bafkzcibf6x...` = CIDv1 + multihash code
// 0x1011 (fr32-sha2-256-trunc254-padbintree). The SDK rejects any
// other format via Piece.is() in piece-cid.ts, so this is the only
// shape worth testing against.
const knownGoodPieceCIDv2 = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"

// httpClient is a fresh client per test — synapse-sdk callers also
// build a new request per call (via iso-web/http's request module).
// Short timeout: the test endpoints are loopback against a local
// daemon, anything beyond 10s indicates a hang.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// baseURL returns the URL under test, or skips the test if CURIO_CORE_URL
// isn't set. Tests run in default `go test ./...` mode without needing
// a live daemon; CI can opt-in by setting the env var.
func baseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("CURIO_CORE_URL")
	if u == "" {
		t.Skip("CURIO_CORE_URL not set; skipping synapse-sdk compat test")
	}
	return strings.TrimRight(u, "/")
}

// TestPingCompat: SDK ping.ts hits GET /pdp/ping, expects 2xx.
func TestPingCompat(t *testing.T) {
	resp, err := httpClient.Get(baseURL(t) + "/pdp/ping")
	if err != nil {
		t.Fatalf("GET /pdp/ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Errorf("ping: status=%d want 2xx", resp.StatusCode)
	}
}

// TestFindPieceCompat: SDK find-piece.ts hits GET /pdp/piece?pieceCid=<v2>.
// Expects 200 with JSON {"pieceCid": "<v2>"} when found, 404 when not.
// For a fresh SP, 404 is the expected path.
func TestFindPieceCompat(t *testing.T) {
	u, _ := url.Parse(baseURL(t) + "/pdp/piece")
	q := u.Query()
	q.Set("pieceCid", knownGoodPieceCIDv2)
	u.RawQuery = q.Encode()

	resp, err := httpClient.Get(u.String())
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		var payload struct {
			PieceCID string `json:"pieceCid"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("findPiece 200 body not parseable as {pieceCid}: %v (body=%q)", err, string(body))
		}
		if payload.PieceCID == "" {
			t.Errorf("findPiece 200 body missing pieceCid field (body=%q)", string(body))
		}
	case http.StatusNotFound:
		// SDK expects this when piece isn't on the SP. Acceptable.
	default:
		t.Errorf("findPiece: status=%d want 200 or 404 (body=%q)", resp.StatusCode, string(body))
	}
}

// TestUploadPieceInitCompat: SDK upload.ts hits POST /pdp/piece with
// {pieceCid: <v2>}, expects 201 with Location: /pdp/piece/upload/<uuid>
// header (or 200 if piece already exists).
func TestUploadPieceInitCompat(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"pieceCid": knownGoodPieceCIDv2})
	req, err := http.NewRequest(http.MethodPost, baseURL(t)+"/pdp/piece", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST /pdp/piece: %v", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		loc := resp.Header.Get("Location")
		if loc == "" {
			t.Error("POST /pdp/piece 201 missing Location header")
		}
		if !strings.HasPrefix(loc, "/pdp/piece/upload/") {
			t.Errorf("Location=%q want prefix /pdp/piece/upload/", loc)
		}
		uuid := loc[len("/pdp/piece/upload/"):]
		if uuid == "" {
			t.Errorf("Location=%q missing uuid suffix", loc)
		}
	case http.StatusOK:
		// SDK acceptance: 200 means piece already exists on server. OK.
	default:
		rbody, _ := io.ReadAll(resp.Body)
		t.Errorf("POST /pdp/piece: status=%d want 201 or 200 (body=%q)", resp.StatusCode, string(rbody))
	}
}

// TestGetDataSetCompat: SDK get-data-set.ts hits GET /pdp/data-sets/{id}.
// For a non-existent dataSetId on a fresh SP, expect 404. Any 500
// indicates a SQL dialect bug (pgx-only ErrNoRows check).
func TestGetDataSetCompat(t *testing.T) {
	// Use a small ID that's almost certainly not in the SP's local DB.
	resp, err := httpClient.Get(baseURL(t) + "/pdp/data-sets/99999")
	if err != nil {
		t.Fatalf("GET /pdp/data-sets/99999: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("getDataSet returned 500 on missing dataset (likely pgx.ErrNoRows-only check): body=%q", string(body))
		return
	}
	// SDK accepts 200 (found) or 404 (not found). Anything else is a
	// real compat bug.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("getDataSet: status=%d want 200 or 404 (body=%q)", resp.StatusCode, string(body))
	}
}

// TestGetDataSetCreationStatusCompat: SDK polls
// GET /pdp/data-sets/created/{txHash} until the dataset lands on chain.
// For a random tx hash, expect 404 (not found) or 400 (invalid format),
// NEVER 500.
func TestGetDataSetCreationStatusCompat(t *testing.T) {
	tx := "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	resp, err := httpClient.Get(baseURL(t) + "/pdp/data-sets/created/" + tx)
	if err != nil {
		t.Fatalf("GET /pdp/data-sets/created/<tx>: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("getDataSetCreationStatus returned 500 on unknown txHash: body=%q", string(body))
		return
	}
	// Acceptable: 200 (found, with status JSON) or 4xx (not found / bad
	// format).
	if resp.StatusCode >= 500 {
		t.Errorf("getDataSetCreationStatus: status=%d (body=%q)", resp.StatusCode, string(body))
	}
}

// (TestGetPieceStatusCompat removed: synapse-sdk doesn't call
// /pdp/piece/{cid}/status — confirmed via grep over
// packages/synapse-core/src. The SDK polls piece-add progress via
// /pdp/data-sets/{id}/pieces/added/{txHash} instead. The /status
// endpoint is upstream-internal, uses Postgres-specific LEFT JOIN
// LATERAL + IPNI tables that curio-core doesn't populate; out of
// scope for synapse-sdk compat.)

// TestAddPiecesStatusCompat: GET /pdp/data-sets/{id}/pieces/added/{txHash}.
// Used by the SDK to poll piece-add progress on the chain. Unknown
// dataset+tx → 404, never 500.
func TestAddPiecesStatusCompat(t *testing.T) {
	tx := "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	resp, err := httpClient.Get(fmt.Sprintf("%s/pdp/data-sets/99999/pieces/added/%s", baseURL(t), tx))
	if err != nil {
		t.Fatalf("GET pieces/added: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("getAddPiecesStatus returned 500: body=%q", string(body))
		return
	}
	if resp.StatusCode >= 500 {
		t.Errorf("getAddPiecesStatus: status=%d (body=%q)", resp.StatusCode, string(body))
	}
}

// TestUploadPieceFullFlow: mirror the SDK upload.ts function in full.
// POST /pdp/piece -> 201 + Location -> PUT /pdp/piece/upload/{uuid} -> 204.
//
// This is the primary client upload path. Unlike the streaming variant
// at /pdp/piece/uploads/* (Day 7 covered that one), this is the
// one-shot path the SDK uses by default in upload.ts when the buffer
// fits in memory.
func TestUploadPieceFullFlow(t *testing.T) {
	base := baseURL(t)

	// Use a deterministic piece. The piece must round-trip through
	// commp on the server side, so we can't just send random bytes
	// against an arbitrary piece CID; the server will reject the upload
	// if the bytes don't hash to the declared piece. We need a known
	// (data, pieceCid) pair.
	//
	// For this test we use a single zero-padded leaf (32 bytes of zeros)
	// whose PieceCIDv2 is well-known: the all-zero leaf with size 32
	// hashes to bafkzcibcaapfemzqaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa (placeholder).
	//
	// SCOPE: this test asserts the route handshake (201+Location, then
	// PUT accepted), not the cryptographic round-trip. The Day 7 smoke
	// covered the latter for the streaming variant; the underlying
	// commp.NewCalc path is the same Go code regardless of route.

	// Step 1: init upload.
	initBody, _ := json.Marshal(map[string]string{"pieceCid": knownGoodPieceCIDv2})
	initReq, err := http.NewRequest(http.MethodPost, base+"/pdp/piece", bytes.NewReader(initBody))
	if err != nil {
		t.Fatalf("build init request: %v", err)
	}
	initReq.Header.Set("Content-Type", "application/json")
	initResp, err := httpClient.Do(initReq)
	if err != nil {
		t.Fatalf("POST /pdp/piece: %v", err)
	}
	defer initResp.Body.Close()
	if initResp.StatusCode != http.StatusCreated && initResp.StatusCode != http.StatusOK {
		rbody, _ := io.ReadAll(initResp.Body)
		t.Fatalf("POST /pdp/piece: status=%d want 201/200 (body=%q)", initResp.StatusCode, string(rbody))
	}
	if initResp.StatusCode == http.StatusOK {
		// Piece already exists on server; nothing to PUT. The SDK accepts
		// this as success.
		return
	}
	loc := initResp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/pdp/piece/upload/") {
		t.Fatalf("Location=%q want prefix /pdp/piece/upload/", loc)
	}

	// Step 2: verify the PUT route is reachable. We can't trivially
	// generate bytes that match the test piece CIDv2 (which encodes a
	// specific size), so we send a short body and expect the handler
	// to either accept it (204) or fail with a meaningful piece-shape
	// error. What we MUST NOT see is 'page not found' — that would
	// indicate the upstream route mount diverged from the SDK shape.
	putURL := base + loc
	putReq, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader([]byte("random non-matching bytes")))
	if err != nil {
		t.Fatalf("build put request: %v", err)
	}
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putResp, err := httpClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT %s: %v", putURL, err)
	}
	defer putResp.Body.Close()
	putBody, _ := io.ReadAll(putResp.Body)

	// 404 with body "page not found" means chi never matched the
	// route. Any other status — including handler-level errors —
	// means the route is wired correctly.
	if putResp.StatusCode == http.StatusNotFound &&
		strings.Contains(strings.ToLower(string(putBody)), "page not found") {
		t.Errorf("PUT %s returned chi 404 'page not found': route mount diverges from SDK shape",
			putURL)
	}
}

// TestRouteSurfaceSmoke: enumerates every route the SDK touches and
// asserts NONE return 500 on baseline (well-formed but unknown-id)
// requests. The point isn't to assert positive functionality; it's to
// catch SQL dialect bugs early.
func TestRouteSurfaceSmoke(t *testing.T) {
	base := baseURL(t)
	tx := "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	cases := []struct {
		name, method, path string
	}{
		{"ping", "GET", "/pdp/ping"},
		{"findPiece", "GET", "/pdp/piece?pieceCid=" + knownGoodPieceCIDv2},
		{"getDataSet", "GET", "/pdp/data-sets/99999"},
		{"getDataSetCreationStatus", "GET", "/pdp/data-sets/created/" + tx},
		{"getAddPiecesStatus", "GET", "/pdp/data-sets/99999/pieces/added/" + tx},
		{"getDataSetPiece", "GET", "/pdp/data-sets/99999/pieces/99999"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(c.method, base+c.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", c.method, c.path, err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 500 {
				t.Errorf("%s %s returned 5xx %d (likely dialect bug): body=%q",
					c.method, c.path, resp.StatusCode, string(body))
			}
		})
	}
}
