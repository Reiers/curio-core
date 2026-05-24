// Package pdpwire constructs the curio/pdp PDPService and mounts its
// HTTP routes onto curio-core's router.
//
// PDPService takes a thick dependency graph in upstream Curio:
//
//	db, paths.StashStore, ethchain.EthClient, PDPServiceNodeApi,
//	*message.SenderETH, *alertmanager.AlertTask, *ipni_provider.Provider
//
// Curio Core's PDP-only deployment shape is much smaller. The
// pdpwire constructor passes nil for what's not yet wired and real
// implementations of everything else. Each nil field means certain
// routes return runtime errors when hit; the upload trio works
// end-to-end today.
//
// The chain-side deps (ethclient + nodeapi + senderEth) are built
// in BuildChainDeps so they can be registered with the harmonytask
// engine BEFORE Start (which takes the impls list up-front). main.go
// is responsible for the ordering: BuildChainDeps -> engine.Start
// (passing deps.SendTaskETH as extra task) -> Mount (binding routes).
package pdpwire

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	upstreampdp "github.com/filecoin-project/curio/pdp"
	pdpcontract "github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	curiopaths "github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/pdpv0"

	"github.com/curiostorage/harmonyquery"

	lanterndaemon "github.com/Reiers/lantern/pkg/daemon"

	"github.com/Reiers/curio-core/internal/diskstash"
	cethclient "github.com/Reiers/curio-core/internal/ethclient"
	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/localpiecepark"
	"github.com/Reiers/curio-core/internal/nodeapi"
	"github.com/Reiers/curio-core/internal/sqliteindex"
)

// Compile-time guards.
var (
	_ curiopaths.StashStore = (*diskstash.Store)(nil)
	_ ethchain.EthClient    = (*cethclient.Client)(nil)
)

// ChainDeps is the set of chain-side dependencies pdpwire needs to
// wire into PDPService. Built before engine.Start so the SendTaskETH
// can be registered with harmonytask up-front.
type ChainDeps struct {
	// NodeAPI is the embedded-Lantern-backed Filecoin RPC client.
	// Implements upstream PDPServiceNodeApi (single ChainHead method
	// today; lotus FullNode handle so it grows transparently).
	NodeAPI upstreampdp.PDPServiceNodeApi

	// EthClient is the embedded-Lantern-backed go-ethereum client.
	// Implements upstream ethchain.EthClient end-to-end.
	EthClient ethchain.EthClient

	// SenderETH signs + broadcasts FEVM transactions with the eth_keys
	// SQLite-stored private key. PDPService uses this for on-chain
	// writes (data-set creation, addPiece, proof submission).
	SenderETH *message.SenderETH

	// SendTaskETH is the harmonytask the engine must register. It
	// drives the SenderETH's send queue.
	SendTaskETH *message.SendTaskETH

	// ChainSync is the singleton pdpv0 task that reconciles deletion
	// state, proven-data-set failure state, and finalized-deletion
	// rails against the on-chain FWSS view. Fires every 8 hours via
	// harmonytask's IAmBored singleton mechanism.
	//
	// Reads: pdp_delete_data_set, pdp_data_set, harmony_task tables;
	// FWSS view contract (FilecoinWarmStorageServiceStateView) via
	// ethClient. Writes: pdp_delete_data_set rows (clearing stale
	// task ids; marking finalized state).
	ChainSync *pdpv0.TaskChainSync

	// ChainSched drives the tipset-subscription event loop that the
	// pdpv0 watcher tasks (DataSetWatch, TerminateServiceWatcher,
	// DataSetDeleteWatcher) hook into via AddHandler. Run() must be
	// invoked AFTER all handlers are registered, exactly once.
	//
	// BuildChainDeps registers the three watcher handlers and returns
	// the scheduler ready-to-run; the engine takes ownership of the
	// goroutine that drives Run(ctx).
	ChainSched *chainsched.CurioChainSched

	// IndexStore is the SQLite-backed pdp_cache_layer storage used by
	// ProveTask + SaveCache. Built atop our harmonysqlite DB; nil only
	// when BuildChainDeps was invoked without a non-nil db (a fast-fail
	// path the lantern-less callers exercise, e.g. tests).
	IndexStore *sqliteindex.Store

	// PieceParkReader serves piece bytes from the diskstash to the
	// cachedreader piece-park fallback path. Satisfies
	// pieceprovider.PieceParkBackend; curio-core's substitute for
	// upstream's cluster-aware *pieceprovider.PieceParkReader.
	PieceParkReader *localpiecepark.Reader

	// CachedPieceReader is the upstream cachedreader.CachedPieceReader
	// constructed with (db, nil-sectorReader, ourPieceParkReader,
	// ourIndexStore). Used by ProveTask + SaveCache.
	CachedPieceReader *cachedreader.CachedPieceReader

	// SaveCache is the upstream PDPv0 task that builds the Merkle layer
	// cache (pdp_cache_layer) for completed pieces. Triggered on tipset
	// apply via the chain scheduler.
	SaveCache *pdpv0.TaskPDPSaveCache

	// ProveTask is the upstream PDPv0 task that submits PDPVerifier
	// challenge proofs on-chain via SenderETH.
	ProveTask *pdpv0.ProveTask

	// InitProvingPeriodTask + NextProvingPeriodTask manage the proving
	// period state machine: setting up the first proving period for a
	// new data set and advancing it after each successful proof.
	InitProvingPeriodTask *pdpv0.InitProvingPeriodTask
	NextProvingPeriodTask *pdpv0.NextProvingPeriodTask

	// Network is the on-chain network the deps target. Threaded into
	// every task constructor that resolves a contract address; also
	// applied to the PDPService via SetNetwork during Mount.
	Network pdpcontract.Network

	// Close releases all transport state. Caller defers this for the
	// process lifetime.
	Close func()
}

// BuildChainDeps dials the embedded Lantern daemon over standard
// /rpc/v1 with a self-minted admin token, constructs the lotus
// FullNode client (for Filecoin.*) and the go-ethereum client (for
// eth_*), and builds a *message.SenderETH bound to the same db the
// engine uses.
//
// Returns nil ChainDeps + nil error when lantern is nil. The caller
// is expected to check d != nil before threading SendTaskETH into the
// engine.
// BuildChainDeps wires every Lantern-backed and pdpv0 task curio-core
// runs. dbConcrete is the underlying *harmonysqlite.DB; stashDir is
// the directory diskstash writes streaming-upload files into.
//
// Returns nil ChainDeps + nil error when lantern is nil. Caller must
// check d != nil before threading the task fields into the engine.
func BuildChainDeps(ctx context.Context, dbConcrete *harmonysqlite.DB, stashDir string, lantern *lanterndaemon.Daemon) (*ChainDeps, error) {
	if lantern == nil {
		return nil, nil
	}
	// db is the interface-typed view of the same handle; used by every
	// upstream task constructor that takes harmonyquery.DBInterface.
	db := harmonyquery.DBInterface(dbConcrete)

	// Derive the on-chain network from the embedded Lantern's runtime
	// config (mainnet | calibration), not from the build-tag-selected
	// constants in upstream curio's pdp/contract package. Without this
	// thread, every contract address (PDPVerifier, FWSService, USDFC,
	// ServiceProviderRegistry) resolves to mainnet at runtime even when
	// the daemon is running with --network calibration. See
	// Reiers/curio@5eb27b0 for the upstream-side refactor that made
	// these network-aware functions available.
	network := pdpNetworkFromLantern(lantern)

	var closeFns []func()
	closer := func() {
		for _, f := range closeFns {
			f()
		}
	}

	nodeC, err := nodeapi.New(ctx, lantern)
	if err != nil {
		return nil, err
	}
	closeFns = append(closeFns, nodeC.Close)

	ethC, err := cethclient.New(ctx, lantern)
	if err != nil {
		closer()
		return nil, err
	}
	closeFns = append(closeFns, ethC.Close)

	senderEth, sendTask := message.NewSenderETH(ethC, db)

	chainSync := pdpv0.NewTaskChainSync(db, ethC, senderEth, network)

	// CurioChainSched: drive a Lotus-style tipset event loop. Lantern's
	// embedded JSON-RPC server is HTTP POST (no WebSocket, no streaming
	// channels), so calling Filecoin.ChainNotify through the standard
	// lotusclient errors with 'method not supported in this mode (no
	// out channel support)'. Lantern V1.5 wires an in-process head-
	// change distributor (Daemon.HeadChanges) that bypasses the RPC
	// transport layer; nodeapi.EmbeddedChainSchedNodeAPI bridges that
	// to the lotus-typed surface chainsched expects.
	//
	// Register the three pdpv0 watcher handlers BEFORE the engine
	// invokes Run() (chainsched.AddHandler rejects after start).
	// Each watcher uses (db, ethClient, sched) and panics on its own
	// AddHandler failure; we trust them not to fail because Run()
	// hasn't been called yet.
	nodeForSched, err := nodeapi.NewEmbeddedChainSchedNodeAPI(nodeC.FullNode(), lantern)
	if err != nil {
		closer()
		return nil, fmt.Errorf("build embedded chainsched node api: %w", err)
	}
	sched := chainsched.New(nodeForSched)
	pdpv0.NewDataSetWatch(db, ethC, sched, network)
	pdpv0.NewTerminateServiceWatcher(db, ethC, sched, network)
	pdpv0.NewDataSetDeleteWatcher(db, ethC, sched, network)
	// Lifecycle sweeper: tipset-driven recovery for #57 (audit on #29).
	// Re-arms datasets after ProveTask MaxFailures exhaustion and logs
	// stuck pdp_piece_uploads where notify cascade-cleared the task_id.
	// Runs on every tipset (~30s) instead of waiting for ChainSync's
	// 8h interval.
	if err := pdpv0.NewLifecycleSweeper(db, sched); err != nil {
		return nil, fmt.Errorf("register lifecycle sweeper: %w", err)
	}

	// The PDP proof loop: SaveCache builds Merkle layers, ProveTask
	// submits possession proofs on-chain, InitPP + NextPP manage the
	// proving-period state machine. All four take cachedreader +
	// indexstore deps that curio-core ships via local-file substitutes
	// for the upstream cluster-aware abstractions:
	//
	//   sqliteindex.Store          satisfies indexstore.Backend on the
	//                              SQLite pdp_cache_layer table
	//   localpiecepark.Reader      satisfies pieceprovider.PieceParkBackend
	//                              over diskstash files
	//   nil sectorReader           cold-storage path never exercised
	//                              for pdpv0 hot-storage pieces
	//
	// The cachedreader's GetSharedPieceReader call chain:
	//   getPieceReaderFromAggregate -> sqliteindex (returns empty;
	//                                  pdpv0 has no aggregates)
	//   getPieceReaderFromMarketPieceDeal -> deals query (empty for pdpv0)
	//                                        -> getPieceReaderFromPiecePark
	//                                        -> localpiecepark.ReadPiece
	idxStore := sqliteindex.New(dbConcrete)
	ppr := localpiecepark.New(dbConcrete, stashDir)
	cpr := cachedreader.NewCachedPieceReader(db, nil /* sectorReader */, ppr, idxStore)

	saveCache := pdpv0.NewTaskPDPSaveCache(db, cpr, idxStore)
	initPP := pdpv0.NewInitProvingPeriodTask(db, ethC, nodeC.FullNode(), sched, senderEth, network)
	nextPP := pdpv0.NewNextProvingPeriodTask(db, ethC, nodeC.FullNode(), sched, senderEth, network)
	proveTask := pdpv0.NewProveTask(sched, db, ethC, nodeC.FullNode(), senderEth, cpr, idxStore, network)

	return &ChainDeps{
		NodeAPI:               nodeC.FullNode(),
		EthClient:             ethC,
		SenderETH:             senderEth,
		SendTaskETH:           sendTask,
		ChainSync:             chainSync,
		ChainSched:            sched,
		IndexStore:            idxStore,
		PieceParkReader:       ppr,
		CachedPieceReader:     cpr,
		SaveCache:             saveCache,
		ProveTask:             proveTask,
		InitProvingPeriodTask: initPP,
		NextProvingPeriodTask: nextPP,
		Network:               network,
		Close:                 closer,
	}, nil
}

// pdpNetworkFromLantern maps the Lantern daemon's runtime network string
// ("mainnet" | "calibration") to the pdp/contract.Network constants
// upstream uses internally. Falls back to mainnet for unknown values
// (matches the upstream build-tag default).
func pdpNetworkFromLantern(d *lanterndaemon.Daemon) pdpcontract.Network {
	switch d.Config().Network {
	case "calibration":
		return pdpcontract.NetworkCalibration
	case "mainnet":
		return pdpcontract.NetworkMainnet
	default:
		return pdpcontract.NetworkMainnet
	}
}

// Mount builds the PDPService and mounts its routes onto r. Pass deps
// from BuildChainDeps (nil-safe).
func Mount(ctx context.Context, r *chi.Mux, db harmonyquery.DBInterface, stashDir string, deps *ChainDeps) (*upstreampdp.PDPService, error) {
	stash, err := diskstash.New(stashDir)
	if err != nil {
		return nil, err
	}

	var (
		nodeAPI   upstreampdp.PDPServiceNodeApi
		ethArg    ethchain.EthClient
		senderEth *message.SenderETH
	)
	if deps != nil {
		nodeAPI = deps.NodeAPI
		ethArg = deps.EthClient
		senderEth = deps.SenderETH
	}

	svc := upstreampdp.NewPDPService(
		ctx,
		db,
		stash,
		ethArg,    // ethchain.EthClient via embedded Lantern /rpc/v1
		nodeAPI,   // PDPServiceNodeApi via embedded Lantern /rpc/v1
		senderEth, // *message.SenderETH — calibration wallet signer
		nil,       // *alertmanager.AlertTask — handlePing nil-checks this
		nil,       // *ipni_provider.Provider — TODO: minimal IPNI publisher
	)
	// Override the build-tag-resolved network (which would default to
	// mainnet for a tagless curio-core build) with the runtime network
	// from the embedded Lantern config. SetNetwork propagates to the
	// pull handler + eth_call validator too.
	if deps != nil && deps.Network != "" {
		svc.SetNetwork(deps.Network)
	}
	upstreampdp.Routes(r, svc)
	return svc, nil
}

// FallbackHandler returns an HTTP handler that serves the chi router
// for /pdp/* and /admin/* paths and delegates everything else to inner.
// Used by cmd/curio-core/main.go to compose the WebUI + PDP routes
// under one listener.
func FallbackHandler(pdpMux *chi.Mux, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isChiPath(r.URL.Path) {
			pdpMux.ServeHTTP(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func isChiPath(p string) bool {
	return hasPathPrefix(p, "/pdp") || hasPathPrefix(p, "/admin")
}

func hasPathPrefix(p, pfx string) bool {
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
