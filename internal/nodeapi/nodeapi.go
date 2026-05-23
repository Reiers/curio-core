// Package nodeapi dials the embedded Lantern daemon over standard
// Lotus-compatible JSON-RPC and exposes the parts curio-core wires
// into upstream curio/pdp.
//
// Architecture: curio-core's embedded Lantern (pkg/daemon.Daemon)
// mounts a /rpc/v1 HTTP endpoint exactly like a standalone Lantern
// would. We talk to it the same way an external SP operator would
// talk to a remote Lantern: through lotus/api/client with a JWT auth
// header. Self-issued at boot via daemon.AdminToken().
//
// Benefits over hand-rolling TipSet construction or borrowing
// internal handler state:
//
//   - Single source of chain truth (no duplicate ChainHead synthesizer
//     that drifts from Lantern's).
//   - Every Filecoin.* and eth_* method Lantern adds in the future
//     becomes available to curio-core for free.
//   - Same wire format external operators consume; bugs surface
//     identically in both deployments.
//   - Standard lotus/api/client gives us a fully-typed FullNode handle.
package nodeapi

import (
	"context"
	"fmt"
	"net/http"

	lotusapi "github.com/filecoin-project/lotus/api"
	lotusclient "github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/go-jsonrpc"

	lanterndaemon "github.com/Reiers/lantern/pkg/daemon"
)

// Client is the curio-core handle to embedded Lantern's JSON-RPC.
// Holds the underlying lotus FullNode RPC client + a Close function
// that releases the HTTP transport.
type Client struct {
	Full  lotusapi.FullNode
	close jsonrpc.ClientCloser
}

// New dials the embedded Lantern Daemon over its in-process
// /rpc/v1 endpoint and self-issues an admin-scope JWT.
//
// Callers must call Close when done to release the HTTP transport.
//
// Returns an error if the Daemon hasn't mounted the RPC server yet
// (Started() == false, or RPCListen was empty).
func New(ctx context.Context, d *lanterndaemon.Daemon) (*Client, error) {
	if d == nil {
		return nil, fmt.Errorf("nodeapi.New: daemon is nil")
	}
	if !d.Started() {
		return nil, fmt.Errorf("nodeapi.New: daemon not started")
	}
	addr := d.RPCAddr()
	if addr == "" {
		return nil, fmt.Errorf("nodeapi.New: daemon has no RPC address (RPCListen empty?)")
	}
	tok := d.AdminToken()
	if tok == "" {
		return nil, fmt.Errorf("nodeapi.New: daemon admin token unavailable")
	}

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+tok)

	url := "http://" + addr + "/rpc/v1"
	full, closer, err := lotusclient.NewFullNodeRPCV1(ctx, url, hdr)
	if err != nil {
		return nil, fmt.Errorf("dial embedded lantern at %s: %w", url, err)
	}
	return &Client{Full: full, close: closer}, nil
}

// Close releases the JSON-RPC transport. Safe to call multiple times.
func (c *Client) Close() {
	if c.close != nil {
		c.close()
		c.close = nil
	}
}

// FullNode returns the underlying lotus FullNode handle. Use this when
// passing curio-core's nodeapi to subsystems that want the full Lotus
// API surface (curio/pdp.PDPServiceNodeApi is one such consumer; it
// only needs ChainHead but the upstream type expects the broader
// interface).
func (c *Client) FullNode() lotusapi.FullNode { return c.Full }
