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

	"github.com/curiostorage/harmonyquery"
)

// Mount constructs a PDPService and mounts its routes onto router r.
//
// The PDPService is constructed with minimal dependencies. Routes that
// require nil dependencies will return 5xx errors at runtime; routes
// that don't (notably /pdp/ping) work end-to-end.
//
// This is the first cut. Subsequent commits expand the deps as they
// come online (paths.StashStore via a curio-core-side stash, EthClient
// pointing at embedded Lantern's RPC, message.SenderETH wrapping a
// calibration wallet, etc.).
func Mount(ctx context.Context, r *chi.Mux, db harmonyquery.DBInterface) *upstreampdp.PDPService {
	svc := upstreampdp.NewPDPService(
		ctx,
		db,
		nil, // paths.StashStore — TODO: curio-core-side stash
		nil, // ethchain.EthClient — TODO: wire to embedded Lantern RPC
		nil, // PDPServiceNodeApi — TODO: wire to embedded Lantern RPC
		nil, // *message.SenderETH — TODO: calibration wallet signer
		nil, // *alertmanager.AlertTask — handlePing nil-checks this
		nil, // *ipni_provider.Provider — TODO: minimal IPNI publisher
	)
	upstreampdp.Routes(r, svc)
	return svc
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
