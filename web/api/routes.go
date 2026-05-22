//go:build cgo

// Package api provides the HTTP API for the lotus curio web gui.
package api

import (
	"github.com/gorilla/mux"

	"github.com/Reiers/curio-core/web/api/config"
	"github.com/Reiers/curio-core/web/api/webrpc"
	"github.com/filecoin-project/curio/deps"
)

func Routes(r *mux.Router, deps *deps.Deps, debug bool) {
	webrpc.Routes(r.PathPrefix("/webrpc").Subrouter(), deps, debug)
	config.Routes(r.PathPrefix("/config").Subrouter(), deps)
	// NOTE(curio-core): /api/sector subrouter dropped — the sealing-era /sector/
	// page it served was removed in the PDP-only WebUI strip. Re-add only if a
	// kept page (PDP, MK20, etc.) starts needing a sector-level API.
}
