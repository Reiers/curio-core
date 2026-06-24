// Package dashboard serves the curio-core operator + client dashboard.
//
// This is the user-facing UI for a running curio-core SP. It exposes:
//
//	GET /              Overview: chain head, datasets, rails, wallet balances
//	GET /wallets       Wallet manager: list/new/import/send (delegates to /admin)
//	GET /datasets      Active datasets with proof status
//	GET /rails         USDFC payment rails with settlement history
//	GET /tasks         harmonytask scheduler health
//	GET /alerts        Alerts feed (mirrors /admin/alerts)
//	GET /api/overview  JSON overview snapshot (auto-refresh consumer)
//	GET /static/*      Embedded SVG logo, fonts, etc.
//
// All HTML is server-side rendered via html/template with Tailwind
// (CDN) + Alpine.js (CDN) for interactivity. No build step.
//
// Auth: same loopback-only model as /admin. nginx in front does NOT
// forward dashboard paths to the public internet; an operator on the
// box reaches it via SSH tunnel or direct loopback.
//
// Tracks: curio-core#39 (P2 hot-storage SP dashboard).
package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("curio-core/dashboard")

//go:embed templates/*.html static/*
var embedded embed.FS

// Server owns the templates + DB handle and renders dashboard pages.
type Server struct {
	db        harmonyquery.DBInterface
	eth       ethchain.EthClient // optional; nil disables balance reads
	tmpl      *template.Template
	cfg       Config
	build     BuildInfo
	usdfcAddr common.Address
	payAddr   common.Address
}

// Config is the runtime knobs the dashboard cares about.
type Config struct {
	// Network is "calibration" or "mainnet"; shown in the header chip.
	Network string

	// Version is the curio-core build version string.
	Version string

	// PayeeAddress is the eth_keys role=pdp wallet — labelled "your SP
	// wallet" in the UI. May be empty before first-run setup completes.
	PayeeAddress string

	// EthClient is the FEVM client used for wallet balance reads and
	// contract calls (USDFC balanceOf, FilecoinPay accounts/getRail).
	// May be nil; the dashboard degrades gracefully.
	EthClient ethchain.EthClient

	// StashDir is the on-disk directory used by parked_pieces. Shown
	// on the storage page so operators can see + verify the location.
	StashDir string

	// DataDir is the curio-core data directory (sqlite + lantern
	// headerstore + token files). Also shown on the storage page.
	DataDir string
}

// BuildInfo is the lightweight subset of build metadata we show in the
// footer of every page. The fields are wired by NewServer.
type BuildInfo struct {
	Version string
	Network string
}

// NewServer constructs the dashboard from the embedded templates.
func NewServer(db harmonyquery.DBInterface, cfg Config) (*Server, error) {
	tmpl := template.New("").Funcs(funcMap())
	matches, err := fs.Glob(embedded, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("glob dashboard templates: %w", err)
	}
	for _, m := range matches {
		b, err := embedded.ReadFile(m)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", m, err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(m, "templates/"), ".html")
		if _, err := tmpl.New(name).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("parse %s: %w", m, err)
		}
	}
	s := &Server{
		db:   db,
		eth:  cfg.EthClient,
		tmpl: tmpl,
		cfg:  cfg,
		build: BuildInfo{
			Version: cfg.Version,
			Network: cfg.Network,
		},
	}
	// Resolve USDFC + FilecoinPay addresses for the active network.
	// Failures are non-fatal; the panels just render "—" when missing.
	if u, err := contract.USDFCAddressFor(contract.Network(cfg.Network)); err == nil {
		s.usdfcAddr = u
	}
	if p, err := filecoinpayment.PaymentContractAddressFor(contract.Network(cfg.Network)); err == nil {
		s.payAddr = p
	}
	return s, nil
}

// Routes mounts the dashboard routes on r.
func (s *Server) Routes(r chi.Router) {
	r.Get("/", s.handleOverview)
	r.Get("/wallets", s.handleWallets)
	r.Get("/datasets", s.handleDatasets)
	r.Get("/rails", s.handleRails)
	r.Get("/tasks", s.handleTasks)
	r.Get("/messages", s.handleMessages)
	r.Get("/alerts", s.handleAlerts)
	r.Get("/storage", s.handleStorage)
	r.Get("/upload", s.handleUploadPage)
	r.Get("/terminal", s.handleTerminalPage)
	r.Get("/api/overview", s.handleAPIOverview)
	r.Post("/api/run", s.handleAPIRun)

	// Static assets (logo, wordmark, favicon).
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(mustSub(embedded, "static")))))
	// Favicon directly off the logo for browsers that probe /favicon.ico.
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		b, err := embedded.ReadFile("static/logo-dark.svg")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(b)
	})
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// ----- handlers -------------------------------------------------------

type pageData struct {
	Title  string
	Build  BuildInfo
	Cfg    Config
	Active string // which nav item is active: "overview", "wallets", ...
	Data   any
}

func (s *Server) render(w http.ResponseWriter, name string, title string, active string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	pd := pageData{
		Title:  title,
		Build:  s.build,
		Cfg:    s.cfg,
		Active: active,
		Data:   data,
	}
	if err := s.tmpl.ExecuteTemplate(w, name, pd); err != nil {
		log.Errorw("render", "name", name, "err", err)
		// At this point headers may already be sent; best effort.
		_, _ = fmt.Fprintf(w, "<!-- render error: %v -->", err)
	}
}

type overviewData struct {
	NowUTC string
	Chain  overviewChain
	Stats  overviewStats
}

type overviewChain struct {
	HeadEpoch int64
	NetworkID string

	// Chain Connectivity + Chain Node Network panels, served entirely off
	// the embedded Lantern = a live zero-Glif proof.
	// All fields are best-effort; a nil/unavailable eth client leaves
	// them at zero/empty and the panel renders graceful placeholders.
	RPCAddress   string // embedded Lantern in-process RPC endpoint
	Reachable    bool   // eth client answered BlockNumber within timeout
	Synced       bool   // head epoch advanced / non-zero (live)
	Version      string // curio-core build version (carries the chip)
	ChainID      int64  // FEVM chain id (314 mainnet / 314159 calibration)
	Peers        int64  // libp2p peer count from the embedded node
	PendingTxCnt int64  // local mpool pending tx count
}

type overviewStats struct {
	DatasetsActive       int64
	DatasetsTerminated   int64
	PiecesCompleteCount  int64
	PiecesCompleteBytes  int64
	RailsActive          int64
	RailsTerminated      int64
	RecentProveSuccess24 int64
	RecentProveFailed24  int64
	TasksRunningNow      int64
	TasksUnowned         int64
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	d := s.collectOverview(r.Context())
	s.render(w, "overview", "Overview", "overview", d)
}

func (s *Server) handleAPIOverview(w http.ResponseWriter, r *http.Request) {
	d := s.collectOverview(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(d)
}

func (s *Server) collectOverview(ctx context.Context) overviewData {
	out := overviewData{
		NowUTC: time.Now().UTC().Format(time.RFC3339),
		Chain: overviewChain{
			NetworkID:  s.cfg.Network,
			Version:    s.cfg.Version,
			RPCAddress: "lantern (embedded, in-process)",
		},
	}
	// Chain head: read from the embedded Lantern via eth_blockNumber
	// (returns the Filecoin chain epoch directly on Lantern). Fall
	// back to MAX(prev_challenge_request_epoch) only if the eth client
	// is unwired.
	if s.eth != nil {
		ctxH, cancel := context.WithTimeout(ctx, 3*time.Second)
		if n, err := s.eth.BlockNumber(ctxH); err == nil {
			out.Chain.HeadEpoch = int64(n)
			out.Chain.Reachable = true
			out.Chain.Synced = n > 0
		}
		cancel()

		// Chain Node Network panel: peer count, chain id, pending mpool.
		// All best-effort off the embedded node; failures leave zeros.
		ctxN, cancelN := context.WithTimeout(ctx, 3*time.Second)
		if p, err := s.eth.PeerCount(ctxN); err == nil {
			out.Chain.Peers = int64(p)
		}
		if id, err := s.eth.ChainID(ctxN); err == nil && id != nil {
			out.Chain.ChainID = id.Int64()
		}
		if pc, err := s.eth.PendingTransactionCount(ctxN); err == nil {
			out.Chain.PendingTxCnt = int64(pc)
		}
		cancelN()
	}
	if out.Chain.HeadEpoch == 0 {
		var head sqlNullInt64
		_ = s.db.QueryRowI(ctx,
			`SELECT MAX(prev_challenge_request_epoch) FROM pdp_data_sets`).Scan(&head)
		out.Chain.HeadEpoch = head.Int64
	}

	var (
		dsActive, dsTerminated int64
	)
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM pdp_data_sets WHERE COALESCE(terminated_at_epoch,0) = 0`).Scan(&dsActive)
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM pdp_data_sets WHERE COALESCE(terminated_at_epoch,0) > 0`).Scan(&dsTerminated)
	out.Stats.DatasetsActive = dsActive
	out.Stats.DatasetsTerminated = dsTerminated

	var (
		piecesCount int64
		piecesBytes sqlNullInt64
	)
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*), SUM(piece_raw_size) FROM parked_pieces WHERE complete=1`).Scan(&piecesCount, &piecesBytes)
	out.Stats.PiecesCompleteCount = piecesCount
	out.Stats.PiecesCompleteBytes = piecesBytes.Int64

	var railsActive, railsTerminated int64
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM pdp_payment_rails WHERE terminated=0`).Scan(&railsActive)
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM pdp_payment_rails WHERE terminated=1`).Scan(&railsTerminated)
	out.Stats.RailsActive = railsActive
	out.Stats.RailsTerminated = railsTerminated

	var proveOK, proveFail int64
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM harmony_task_history WHERE name='PDPv0_Prove' AND result=1 AND work_end >= datetime('now','-24 hours')`).Scan(&proveOK)
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM harmony_task_history WHERE name='PDPv0_Prove' AND result=0 AND work_end >= datetime('now','-24 hours')`).Scan(&proveFail)
	out.Stats.RecentProveSuccess24 = proveOK
	out.Stats.RecentProveFailed24 = proveFail

	var tasksRunning, tasksUnowned int64
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM harmony_task WHERE owner_id IS NOT NULL`).Scan(&tasksRunning)
	_ = s.db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM harmony_task WHERE owner_id IS NULL`).Scan(&tasksUnowned)
	out.Stats.TasksRunningNow = tasksRunning
	out.Stats.TasksUnowned = tasksUnowned

	return out
}

type walletsData struct {
	Wallets []walletRow
}

type walletRow struct {
	Address string
	Role    string
	IsPDP   bool
	FILWei  string // decimal display, 18 decimals, empty if unknown
	USDFC   string // decimal display, 18 decimals, empty if unknown
}

func (s *Server) handleWallets(w http.ResponseWriter, r *http.Request) {
	d := walletsData{}
	var rows []struct {
		Address string `db:"address"`
		Role    string `db:"role"`
	}
	if err := s.db.SelectI(r.Context(), &rows,
		`SELECT address, role FROM eth_keys ORDER BY role, address`); err == nil {
		for _, row := range rows {
			filBal, usdfcBal := s.readBalances(r.Context(), row.Address)
			d.Wallets = append(d.Wallets, walletRow{
				Address: row.Address,
				Role:    row.Role,
				IsPDP:   row.Role == "pdp",
				FILWei:  filBal,
				USDFC:   usdfcBal,
			})
		}
	}
	s.render(w, "wallets", "Wallets", "wallets", d)
}

// readBalances reads the native FIL balance + USDFC ERC-20 balance for
// an eth address. Returns ("", "") if the eth client isn't wired or
// the address is unparseable.
func (s *Server) readBalances(ctx context.Context, addr string) (filDec, usdfcDec string) {
	if s.eth == nil || !common.IsHexAddress(addr) {
		return "", ""
	}
	a := common.HexToAddress(addr)
	ctx2, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if fil, err := s.eth.BalanceAt(ctx2, a, nil); err == nil && fil != nil {
		filDec = decimal18(fil)
	}
	if s.usdfcAddr != (common.Address{}) {
		// balanceOf(address) selector 0x70a08231
		var data [4 + 32]byte
		data[0] = 0x70
		data[1] = 0xa0
		data[2] = 0x82
		data[3] = 0x31
		copy(data[4+12:], a.Bytes())
		res, err := s.eth.CallContract(ctx2, ethereum.CallMsg{
			To:   &s.usdfcAddr,
			Data: data[:],
		}, nil)
		if err == nil && len(res) == 32 {
			n := new(big.Int).SetBytes(res)
			usdfcDec = decimal18(n)
		}
	}
	return
}

// decimal18 renders a wei-style big.Int as a decimal string with 18
// fractional digits, trimming trailing zeros.
func decimal18(n *big.Int) string {
	if n == nil || n.Sign() == 0 {
		return "0"
	}
	neg := n.Sign() < 0
	abs := new(big.Int).Abs(n)
	eighteen := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	intPart := new(big.Int).Quo(abs, eighteen)
	fracPart := new(big.Int).Mod(abs, eighteen)
	out := intPart.String()
	if fracPart.Sign() != 0 {
		fracStr := fmt.Sprintf("%018s", fracPart.String())
		fracStr = strings.TrimRight(fracStr, "0")
		out = out + "." + fracStr
	}
	if neg {
		out = "-" + out
	}
	return out
}

type datasetsData struct {
	Datasets []datasetRow
}

type datasetRow struct {
	ID                      int64        `db:"id"`
	ProveAtEpoch            sqlNullInt64 `db:"prove_at_epoch"`
	PrevChallengeReqEpoch   sqlNullInt64 `db:"prev_challenge_request_epoch"`
	ConsecutiveProveFailure int64        `db:"consecutive_prove_failures"`
	TerminatedAtEpoch       sqlNullInt64 `db:"terminated_at_epoch"`
	InitReady               int64        `db:"init_ready"`
}

func (s *Server) handleDatasets(w http.ResponseWriter, r *http.Request) {
	d := datasetsData{}
	_ = s.db.SelectI(r.Context(), &d.Datasets,
		`SELECT id, prove_at_epoch, prev_challenge_request_epoch,
			consecutive_prove_failures, terminated_at_epoch, init_ready
		FROM pdp_data_sets ORDER BY id DESC LIMIT 200`)
	s.render(w, "datasets", "Datasets", "datasets", d)
}

type railsData struct {
	Rails             []railRow
	TotalRatePerEpoch string // sum of paymentRate across non-terminated rails
}

type railRow struct {
	RailID           int64         `db:"rail_id"`
	Payer            string        `db:"payer"`
	LastSettledEpoch int64         `db:"last_settled_epoch"`
	Terminated       int64         `db:"terminated"`
	EndEpoch         int64         `db:"end_epoch"`
	LastSettleTxHash sqlNullString `db:"last_settle_tx_hash"`
	LastSettleError  sqlNullString `db:"last_settle_error"`
	LastSeenAt       string        `db:"last_seen_at"`
	LastSettledAt    sqlNullString `db:"last_settled_at"`

	// Enriched on-chain reads (not persisted; populated per request).
	PaymentRate string // USDFC per epoch, 18-decimal display
	SettledUpTo int64
}

func (s *Server) handleRails(w http.ResponseWriter, r *http.Request) {
	d := railsData{}
	_ = s.db.SelectI(r.Context(), &d.Rails,
		`SELECT rail_id, payer, last_settled_epoch, terminated, end_epoch,
			last_settle_tx_hash, last_settle_error, last_seen_at, last_settled_at
		FROM pdp_payment_rails ORDER BY terminated ASC, rail_id ASC`)

	// Enrich each row with on-chain getRail data: paymentRate +
	// settledUpTo, used to display incoming-rate and pending balance.
	// Best effort — if the eth client or contract address is unwired,
	// the table still renders without these columns populated.
	if s.eth != nil && s.payAddr != (common.Address{}) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		pay, err := filecoinpayment.NewPayments(s.payAddr, s.eth)
		if err == nil {
			var totalRateWei = new(big.Int)
			var totalPendingWei = new(big.Int)
			for i := range d.Rails {
				r := &d.Rails[i]
				if r.Terminated > 0 {
					continue
				}
				view, gErr := pay.GetRail(&bind.CallOpts{Context: ctx}, big.NewInt(r.RailID))
				if gErr != nil {
					continue
				}
				r.PaymentRate = decimal18(view.PaymentRate)
				r.SettledUpTo = view.SettledUpTo.Int64()
				totalRateWei.Add(totalRateWei, view.PaymentRate)
				// Pending = paymentRate * (currentEpoch - settledUpTo).
				// We don't have currentEpoch here without another call;
				// approximate using the most recent dataset's
				// prev_challenge_request_epoch from overview. For V1 we
				// just show the rate; pending stays as a 0 placeholder.
			}
			d.TotalRatePerEpoch = decimal18(totalRateWei)
			_ = totalPendingWei
		}
	}

	s.render(w, "rails", "Payment Rails", "rails", d)
}

type tasksData struct {
	Active []taskRow
	Recent []taskHistRow
}

type taskRow struct {
	ID         int64         `db:"id"`
	Name       string        `db:"name"`
	PostedTime sqlNullString `db:"posted_time"`
	OwnerID    sqlNullInt64  `db:"owner_id"`
}

type taskHistRow struct {
	ID       int64  `db:"id"`
	TaskID   int64  `db:"task_id"`
	Name     string `db:"name"`
	Posted   string `db:"posted"`
	WorkEnd  string `db:"work_end"`
	Result   int64  `db:"result"`
	ErrShort string `db:"err_short"`
	Executor string `db:"executor"`
	// Queued = work_start - posted (ms); Took = work_end - work_start (ms).
	// Computed in SQLite via julianday so the template just prints them.
	QueuedMS int64 `db:"queued_ms"`
	TookMS   int64 `db:"took_ms"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	d := tasksData{}
	_ = s.db.SelectI(r.Context(), &d.Active,
		`SELECT id, name, posted_time, owner_id FROM harmony_task
		 ORDER BY id DESC LIMIT 50`)
	_ = s.db.SelectI(r.Context(), &d.Recent,
		`SELECT id, task_id, name,
		        substr(posted,1,19)   AS posted,
		        substr(work_end,1,19) AS work_end,
		        result,
		        substr(COALESCE(err,''),1,80)                       AS err_short,
		        COALESCE(completed_by_host_and_port,'')             AS executor,
		        CAST((julianday(work_start)-julianday(posted))   *86400000 AS INTEGER) AS queued_ms,
		        CAST((julianday(work_end)  -julianday(work_start))*86400000 AS INTEGER) AS took_ms
		 FROM harmony_task_history
		 ORDER BY id DESC LIMIT 50`)
	s.render(w, "tasks", "Tasks", "tasks", d)
}

type alertsData struct {
	Alerts []alertRow
}

type alertRow struct {
	ID       int64         `db:"id"`
	Title    string        `db:"title"`
	Message  sqlNullString `db:"message"`
	Severity string        `db:"severity"`
	Ack      int64         `db:"ack"`
	Created  string        `db:"created_at"`
}

type storageData struct {
	StashDir         string
	DataDir          string
	StashSizeBytes   int64
	StashSizeErr     string
	PiecesComplete   int64
	PiecesIncomplete int64
	PiecesBytes      int64
}

func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	d := storageData{
		StashDir: s.cfg.StashDir,
		DataDir:  s.cfg.DataDir,
	}
	// piece counts + bytes
	_ = s.db.QueryRowI(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(piece_raw_size),0) FROM parked_pieces WHERE complete=1`).
		Scan(&d.PiecesComplete, &d.PiecesBytes)
	_ = s.db.QueryRowI(r.Context(),
		`SELECT COUNT(*) FROM parked_pieces WHERE complete=0`).Scan(&d.PiecesIncomplete)
	// physical stash size
	if s.cfg.StashDir != "" {
		if sz, err := dirSize(s.cfg.StashDir); err == nil {
			d.StashSizeBytes = sz
		} else {
			d.StashSizeErr = err.Error()
		}
	}
	s.render(w, "storage", "Storage", "storage", d)
}

type uploadData struct {
	ListenURL  string
	DaemonAddr string
	HasPDPKey  bool
}

func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	d := uploadData{
		HasPDPKey: s.cfg.PayeeAddress != "",
	}
	s.render(w, "upload", "Upload", "upload", d)
}

type terminalData struct {
	AllowedCommands []string
}

func (s *Server) handleTerminalPage(w http.ResponseWriter, r *http.Request) {
	d := terminalData{
		AllowedCommands: allowlistedSubcommands(),
	}
	s.render(w, "terminal", "Terminal", "terminal", d)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	d := alertsData{}
	_ = s.db.SelectI(r.Context(), &d.Alerts,
		`SELECT id, title, message, severity, ack, created_at
		 FROM alerts ORDER BY ack ASC, id DESC LIMIT 100`)
	s.render(w, "alerts", "Alerts", "alerts", d)
}

type messageRow struct {
	Hash        string        `db:"signed_tx_hash"`
	Reason      sqlNullString `db:"send_reason"`
	FromAddr    sqlNullString `db:"from_address"`
	ToAddr      sqlNullString `db:"to_address"`
	Nonce       sqlNullInt64  `db:"nonce"`
	Status      sqlNullString `db:"tx_status"`
	Success     sqlNullInt64  `db:"tx_success"`
	Block       sqlNullInt64  `db:"confirmed_block_number"`
	SendTime    sqlNullString `db:"send_time"`
	SendErr     sqlNullString `db:"send_error"`
	GasUsed     sqlNullInt64  `db:"gas_used"`
	GasPriceWei sqlNullString `db:"gas_price_wei"`
	CostFIL     string        // computed: gas_used * gas_price, formatted
	StateLabel  string        // computed: pending | success | REVERTED | send-failed
}

type messagesData struct {
	Pending  []messageRow
	History  []messageRow
	Reverted int
}

// handleMessages renders the mpool / message view: pending (in-flight) txs,
// confirmed history with on-chain success/REVERT status, nonce, and cost.
// This is the operator-honest view: a task can report success while its tx
// reverted on-chain, and that REVERT is shown here in red (and alerted).
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	d := messagesData{}

	// Pending = sent locally, not yet confirmed (or never tracked as confirmed).
	_ = s.db.SelectI(r.Context(), &d.Pending, `
		SELECT s.signed_hash AS signed_tx_hash,
		       s.send_reason  AS send_reason,
		       s.from_address AS from_address,
		       s.to_address   AS to_address,
		       s.nonce        AS nonce,
		       COALESCE(w.tx_status,'pending') AS tx_status,
		       w.tx_success   AS tx_success,
		       w.confirmed_block_number AS confirmed_block_number,
		       s.send_time    AS send_time,
		       s.send_error   AS send_error,
		       NULL AS gas_used, NULL AS gas_price_wei
		FROM message_sends_eth s
		LEFT JOIN message_waits_eth w ON lower(w.signed_tx_hash)=lower(s.signed_hash)
		WHERE s.signed_hash IS NOT NULL
		  AND (w.tx_status IS NULL OR w.tx_status='pending')
		ORDER BY s.nonce DESC LIMIT 50`)

	// History = confirmed (success or revert), newest first.
	_ = s.db.SelectI(r.Context(), &d.History, `
		SELECT w.signed_tx_hash AS signed_tx_hash,
		       s.send_reason  AS send_reason,
		       s.from_address AS from_address,
		       s.to_address   AS to_address,
		       s.nonce        AS nonce,
		       w.tx_status    AS tx_status,
		       w.tx_success   AS tx_success,
		       w.confirmed_block_number AS confirmed_block_number,
		       s.send_time    AS send_time,
		       s.send_error   AS send_error,
		       NULL AS gas_used, NULL AS gas_price_wei
		FROM message_waits_eth w
		LEFT JOIN message_sends_eth s ON lower(s.signed_hash)=lower(w.signed_tx_hash)
		WHERE w.tx_status='confirmed' OR w.tx_status='failed'
		ORDER BY w.confirmed_block_number DESC, w.rowid DESC LIMIT 100`)

	label := func(m *messageRow) {
		switch {
		case m.SendErr.Valid && m.SendErr.String != "":
			m.StateLabel = "send-failed"
		case !m.Status.Valid || m.Status.String == "pending":
			m.StateLabel = "pending"
		case m.Success.Valid && m.Success.Int64 == 0:
			m.StateLabel = "REVERTED"
		case m.Success.Valid && m.Success.Int64 == 1:
			m.StateLabel = "success"
		default:
			m.StateLabel = m.Status.String
		}
	}
	for i := range d.Pending {
		label(&d.Pending[i])
	}
	for i := range d.History {
		label(&d.History[i])
		if d.History[i].StateLabel == "REVERTED" {
			d.Reverted++
		}
	}
	s.render(w, "messages", "Messages", "messages", d)
}

// ----- helpers --------------------------------------------------------

type sqlNullInt64 struct {
	Int64 int64
	Valid bool
}

func (n *sqlNullInt64) Scan(src any) error {
	if src == nil {
		n.Valid = false
		n.Int64 = 0
		return nil
	}
	switch v := src.(type) {
	case int64:
		n.Int64 = v
	case []byte:
		i, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return err
		}
		n.Int64 = i
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return err
		}
		n.Int64 = i
	case float64:
		n.Int64 = int64(v)
	default:
		return fmt.Errorf("sqlNullInt64: cannot scan %T", src)
	}
	n.Valid = true
	return nil
}

func (n sqlNullInt64) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return []byte(strconv.FormatInt(n.Int64, 10)), nil
}

type sqlNullString struct {
	String string
	Valid  bool
}

func (n *sqlNullString) Scan(src any) error {
	if src == nil {
		return nil
	}
	switch v := src.(type) {
	case string:
		n.String = v
	case []byte:
		n.String = string(v)
	default:
		return fmt.Errorf("sqlNullString: cannot scan %T", src)
	}
	n.Valid = true
	return nil
}

func (n sqlNullString) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	b, _ := json.Marshal(n.String)
	return b, nil
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"shortAddr": func(s string) string {
			if len(s) < 10 {
				return s
			}
			return s[:6] + "…" + s[len(s)-4:]
		},
		"shortHash": func(s string) string {
			if len(s) < 12 {
				return s
			}
			return s[:8] + "…" + s[len(s)-6:]
		},
		"humanBytes": func(n int64) string {
			if n <= 0 {
				return "0 B"
			}
			const u = 1024
			if n < u {
				return fmt.Sprintf("%d B", n)
			}
			div, exp := int64(u), 0
			for nn := n / u; nn >= u; nn /= u {
				div *= u
				exp++
			}
			suffix := "KMGTPE"[exp]
			return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), suffix)
		},
		"epochAge": func(epoch int64, head int64) string {
			if epoch <= 0 || head <= 0 {
				return "—"
			}
			d := head - epoch
			if d < 0 {
				return fmt.Sprintf("%d epochs ahead", -d)
			}
			secs := d * 30
			switch {
			case secs < 60:
				return fmt.Sprintf("%ds ago", secs)
			case secs < 3600:
				return fmt.Sprintf("%dm ago", secs/60)
			case secs < 86400:
				return fmt.Sprintf("%dh %dm ago", secs/3600, (secs%3600)/60)
			default:
				return fmt.Sprintf("%dd %dh ago", secs/86400, (secs%86400)/3600)
			}
		},
		"yesno": func(b int64) string {
			if b > 0 {
				return "yes"
			}
			return "no"
		},
		"bigZero": func(s string) string {
			if s == "" || s == "0" {
				return "—"
			}
			n, ok := new(big.Int).SetString(s, 10)
			if !ok {
				return s
			}
			// USDFC uses 18 decimals.
			eighteen := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
			intPart := new(big.Int).Quo(n, eighteen)
			fracPart := new(big.Int).Mod(n, eighteen)
			if fracPart.Sign() == 0 {
				return intPart.String()
			}
			fracStr := fmt.Sprintf("%018s", fracPart.String())
			fracStr = strings.TrimRight(fracStr, "0")
			return intPart.String() + "." + fracStr
		},
		"add": func(a, b int) int { return a + b },
		"durMS": func(ms int64) string {
			if ms < 0 {
				return "—"
			}
			if ms < 1000 {
				return fmt.Sprintf("%dms", ms)
			}
			if ms < 60000 {
				return fmt.Sprintf("%.1fs", float64(ms)/1000)
			}
			return fmt.Sprintf("%dm %ds", ms/60000, (ms%60000)/1000)
		},
	}
}
