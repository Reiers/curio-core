// Package pdpwire constructs the curio/pdp PDPService and mounts its
// HTTP routes onto curio-core's router.
//
// PDPService takes a thick dependency graph in upstream Curio:
//
//	db, paths.StashStore, ethchain.EthClient, PDPServiceNodeApi,
//	*message.SenderETH, *alertmanager.AlertTask, *ipni_provider.Provider
//
// Curio Core's PDP-only deployment shape is much smaller. For the
// initial demo (Sat 2026-05-23, real PDP cycle: Mac client → Hetzner
// curio-core provider), this constructor passes nil for the heavy
// optional fields and a real db. Each nil field means certain routes
// will return runtime errors when hit; that's acceptable while we
// incrementally wire the rest. The /pdp/ping route works today because
// it only references p.Auth and (nilable) p.alertTask.
//
// As more of the demo lights up, this file grows to pass real
// implementations of each subsystem. The interface boundary stays
// stable; what changes is the constructor body.
package pdpwire

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	upstreampdp "github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/lib/ethchain"
	curiopaths "github.com/filecoin-project/curio/lib/paths"

	"github.com/curiostorage/harmonyquery"

	lanterndaemon "github.com/Reiers/lantern/pkg/daemon"

	"github.com/Reiers/curio-core/internal/diskstash"
	cethclient "github.com/Reiers/curio-core/internal/ethclient"
	"github.com/Reiers/curio-core/internal/nodeapi"
)

// Compile-time guard: *diskstash.Store must satisfy paths.StashStore.
var _ curiopaths.StashStore = (*diskstash.Store)(nil)

// Mount constructs a PDPService and mounts its routes onto router r.
//
// stashDir is the local-disk path where /pdp/piece/upload* streams
// piece bytes before SQLite registration. Created with 0o700 if not
// present.
//
// The PDPService is constructed with the minimum deps to drive the
// upload-side flow:
//   - db: SQLite (via harmonyquery interface)
//   - storage: local-disk *diskstash.Store implementing paths.StashStore
//
// Heavy chain-side deps (ethchain.EthClient, PDPServiceNodeApi,
// *message.SenderETH, *ipni_provider.Provider) remain nil for now.
// Routes that nil-deref them (data-set creation, addPiece on-chain,
// proof submission) will return 5xx; the upload trio (/pdp/piece/uploads*)
// works end-to-end and lands data on disk + a row in
// pdp_piece_streaming_uploads.
// lantern is the embedded Lantern daemon. May be nil for tests or
// the --no-lantern boot path; routes that need chain reads degrade
// to 5xx in that mode.
//
// When lantern is non-nil and Started, Mount dials it over standard
// /rpc/v1 with a self-minted admin token and wires the resulting
// FullNode handle into upstream PDPService as the PDPServiceNodeApi.
//
// Returns a closer that releases the JSON-RPC transport; callers
// should defer it for the lifetime of the daemon.
func Mount(ctx context.Context, r *chi.Mux, db harmonyquery.DBInterface, stashDir string, lantern *lanterndaemon.Daemon) (*upstreampdp.PDPService, func(), error) {
	stash, err := diskstash.New(stashDir)
	if err != nil {
		return nil, func() {}, err
	}
	var (
		nodeAPI upstreampdp.PDPServiceNodeApi
		ethC    *cethclient.Client
		closeFns []func()
	)
	if lantern != nil {
		nodeC, err := nodeapi.New(ctx, lantern)
		if err != nil {
			return nil, func() {}, err
		}
		nodeAPI = nodeC.FullNode()
		closeFns = append(closeFns, nodeC.Close)

		ethC, err = cethclient.New(ctx, lantern)
		if err != nil {
			nodeC.Close()
			return nil, func() {}, err
		}
		closeFns = append(closeFns, ethC.Close)
	}
	closer := func() {
		for _, f := range closeFns {
			f()
		}
	}

	// nil-safe handoff: when lantern was nil, ethC is also nil. PDPService
	// accepts a nil ethchain.EthClient; routes that touch chain reads
	// return 5xx in that mode (same shape as before today's wiring).
	var ethArg ethchain.EthClient
	if ethC != nil {
		ethArg = ethC
	}
	svc := upstreampdp.NewPDPService(
		ctx,
		db,
		stash,
		ethArg,  // ethchain.EthClient via embedded Lantern /rpc/v1 (eth_*)
		nodeAPI, // PDPServiceNodeApi via embedded Lantern /rpc/v1 (Filecoin.*)
		nil,     // *message.SenderETH — TODO: calibration wallet signer
		nil,     // *alertmanager.AlertTask — handlePing nil-checks this
		nil,     // *ipni_provider.Provider — TODO: minimal IPNI publisher
	)
	upstreampdp.Routes(r, svc)
	return svc, closer, nil
}

// Compile-time guard: *cethclient.Client satisfies ethchain.EthClient.
var _ ethchain.EthClient = (*cethclient.Client)(nil)

// FallbackHandler returns an HTTP handler that serves the chi router
// for /pdp/* paths and delegates everything else to inner. Used by
// cmd/curio-core/main.go to compose the WebUI + PDP routes under one
// listener.
func FallbackHandler(pdpMux *chi.Mux, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPDPPath(r.URL.Path) {
			pdpMux.ServeHTTP(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func isPDPPath(p string) bool {
	const pfx = "/pdp"
	if len(p) < len(pfx) {
		return false
	}
	if p[:len(pfx)] != pfx {
		return false
	}
	if len(p) == len(pfx) {
		return true
	}
	return p[len(pfx)] == '/'
}
