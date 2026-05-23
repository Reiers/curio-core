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
	curiopaths "github.com/filecoin-project/curio/lib/paths"

	"github.com/curiostorage/harmonyquery"

	"github.com/Reiers/curio-core/internal/diskstash"
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
func Mount(ctx context.Context, r *chi.Mux, db harmonyquery.DBInterface, stashDir string) (*upstreampdp.PDPService, error) {
	stash, err := diskstash.New(stashDir)
	if err != nil {
		return nil, err
	}
	svc := upstreampdp.NewPDPService(
		ctx,
		db,
		stash,
		nil, // ethchain.EthClient — TODO: wire to embedded Lantern RPC
		nil, // PDPServiceNodeApi — TODO: wire to embedded Lantern RPC
		nil, // *message.SenderETH — TODO: calibration wallet signer
		nil, // *alertmanager.AlertTask — handlePing nil-checks this
		nil, // *ipni_provider.Provider — TODO: minimal IPNI publisher
	)
	upstreampdp.Routes(r, svc)
	return svc, nil
}

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
