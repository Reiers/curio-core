// Package ethclient dials the embedded Lantern daemon's eth_* JSON-RPC
// surface and exposes a go-ethereum *ethclient.Client. Upstream
// curio/pdp wants an ethchain.EthClient (a superset of go-ethereum's
// ethclient interface); this package gives us the standard
// ethclient.Client which satisfies the bulk of those methods.
//
// Architecture: same as internal/nodeapi but for the eth side.
// Curio-core dials embedded Lantern at d.RPCAddr()/rpc/v1 over
// standard HTTP JSON-RPC with the self-minted admin token. Lantern
// publishes eth_call, eth_getBalance, eth_getBlockByNumber et al. on
// the same /rpc/v1 endpoint as Filecoin.*. go-ethereum's rpc.Client
// + ethclient.NewClient ride right on top.
package ethclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	lanterndaemon "github.com/Reiers/lantern/pkg/daemon"
)

// Client wraps go-ethereum's *ethclient.Client + the underlying
// rpc.Client so callers can Close cleanly.
type Client struct {
	*ethclient.Client
	rpcClient *rpc.Client
}

// New dials the embedded Lantern Daemon and returns an *ethclient.Client
// authenticated with the daemon's admin-scope JWT. The returned client
// implements the standard go-ethereum ethclient surface (which covers
// the bulk of upstream curio's ethchain.EthClient interface for read
// methods).
//
// Callers must call Close when done to release the HTTP transport.
//
// Returns an error if the Daemon hasn't mounted its RPC server yet.
func New(ctx context.Context, d *lanterndaemon.Daemon) (*Client, error) {
	if d == nil {
		return nil, fmt.Errorf("ethclient.New: daemon is nil")
	}
	if !d.Started() {
		return nil, fmt.Errorf("ethclient.New: daemon not started")
	}
	addr := d.RPCAddr()
	if addr == "" {
		return nil, fmt.Errorf("ethclient.New: daemon has no RPC address (RPCListen empty?)")
	}
	tok := d.AdminToken()
	if tok == "" {
		return nil, fmt.Errorf("ethclient.New: daemon admin token unavailable")
	}

	url := "http://" + addr + "/rpc/v1"
	rpcClient, err := rpc.DialOptions(ctx, url,
		rpc.WithHTTPClient(&http.Client{}),
		rpc.WithHeader("Authorization", "Bearer "+tok),
	)
	if err != nil {
		return nil, fmt.Errorf("dial embedded lantern eth-rpc at %s: %w", url, err)
	}
	return &Client{
		Client:    ethclient.NewClient(rpcClient),
		rpcClient: rpcClient,
	}, nil
}

// Close releases the underlying RPC transport. Safe to call multiple
// times.
func (c *Client) Close() {
	if c.rpcClient != nil {
		c.rpcClient.Close()
		c.rpcClient = nil
	}
}
