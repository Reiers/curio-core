// Package payments runs the USDFC payment-rail discovery + settlement
// loop for the PDP-as-SP role.
//
// FilecoinWarmStorageService creates a FilecoinPay rail per client/dataset
// when storage is provisioned. The SP is the rail's PAYEE and must
// periodically call FilecoinPay.settleRail(railId, untilEpoch) to claim
// the accumulated USDFC. Without settlement the USDFC stays escrowed
// inside FilecoinPay's lockup and never moves to our balance.
//
// Architecture (deliberately small):
//
//   - One singleton harmonytask, PDPv0_PaymentSettle, ~10-min IAmBored.
//   - Discovery via FilecoinPay.getRailsForPayeeAndToken(our_pdp_addr,
//     USDFC, ...). Pure on-chain read, no event-log subscriptions.
//   - For each non-terminated rail, build a settleRail(rail, currentEpoch)
//     tx and hand it to SenderETH with reason="pdp-rail-settle".
//   - Record the tx hash on the pdp_payment_rails row + insert a row in
//     filecoin_payment_transactions for the existing watcher to consume.
//
// What this does NOT do (deferred):
//
//   - No RailSettled event watcher. The contract logs (totalSettledAmount,
//     settledUpTo) on success; we'll add a watcher when V1 monitoring
//     needs the exact accumulation. For now we trust the tx receipt and
//     update last_settled_epoch optimistically on send.
//   - No terminated-rail finalization (settleTerminatedRailWithoutValidation).
//     Skipped for V1; rails that hit endEpoch and stop accruing just
//     stop being settled. Add when FWSS terminates a real rail in
//     production and we need to claim the final segment.
//   - No alert on low balance. Add when wired into the alerts surface.
//
// Tracks: curio-core#37 (P1 hot-storage feature).
package payments

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/pdp/contract"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("curio-core/payments")

// PollInterval is the cadence of the singleton task. 10 minutes is a
// good balance: rails accrue payment continuously but blocks come every
// ~30s, so settling more often gives the operator faster USDFC turnaround
// at the cost of more on-chain gas. Tunable later.
const PollInterval = 10 * time.Minute

// MaxRailsPerDiscovery caps the page size on getRailsForPayeeAndToken.
// 200 is well below any reasonable practical cap; we'll add real
// pagination when we see SPs with that many concurrent clients.
const MaxRailsPerDiscovery = 200

// MaxSettlesPerCycle caps how many settleRail txes we broadcast in
// a single task run. Prevents a backlog blast on first activation.
const MaxSettlesPerCycle = 16

// Task is the harmonytask that discovers + settles USDFC payment rails.
type Task struct {
	db        harmonyquery.DBInterface
	ethClient ethchain.EthClient
	sender    *message.SenderETH
	network   contract.Network

	// payee is the address that receives rail payments. Resolved from
	// eth_keys role='pdp' at task construction.
	payee common.Address

	// payContract is the FilecoinPay binding for the active network.
	payContract     *filecoinpayment.Payments
	payContractAddr common.Address

	// usdfcAddr is the ERC20 token address that FWSS rails settle in.
	usdfcAddr common.Address
}

// New constructs the settlement task. payee is typically the PDP wallet
// address from eth_keys role='pdp'.
func New(
	db harmonyquery.DBInterface,
	ethClient ethchain.EthClient,
	sender *message.SenderETH,
	network contract.Network,
	payee common.Address,
) (*Task, error) {
	payContractAddr, err := filecoinpayment.PaymentContractAddressFor(network)
	if err != nil {
		return nil, fmt.Errorf("resolve FilecoinPay address for %s: %w", network, err)
	}
	usdfcAddr, err := contract.USDFCAddressFor(network)
	if err != nil {
		return nil, fmt.Errorf("resolve USDFC address for %s: %w", network, err)
	}
	payContract, err := filecoinpayment.NewPayments(payContractAddr, ethClient)
	if err != nil {
		return nil, fmt.Errorf("bind FilecoinPay at %s: %w", payContractAddr.Hex(), err)
	}
	return &Task{
		db:              db,
		ethClient:       ethClient,
		sender:          sender,
		network:         network,
		payee:           payee,
		payContract:     payContract,
		payContractAddr: payContractAddr,
		usdfcAddr:       usdfcAddr,
	}, nil
}

// Do runs one discovery + settlement cycle.
func (t *Task) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	// Step 1: discover rails on-chain.
	if !stillOwned() {
		return false, nil
	}
	rails, err := t.discover(ctx)
	if err != nil {
		return false, fmt.Errorf("payments: discover rails: %w", err)
	}
	if len(rails) == 0 {
		log.Debug("payments: no rails for payee yet")
		return true, nil
	}

	// Step 2: upsert into pdp_payment_rails.
	if !stillOwned() {
		return false, nil
	}
	if err := t.upsertRails(ctx, rails); err != nil {
		return false, fmt.Errorf("payments: upsert rails: %w", err)
	}

	// Step 3: figure out which to settle. Skip terminated rails for V1.
	if !stillOwned() {
		return false, nil
	}
	candidates := make([]railInfo, 0, len(rails))
	for _, r := range rails {
		if r.terminated {
			continue
		}
		candidates = append(candidates, r)
		if len(candidates) >= MaxSettlesPerCycle {
			break
		}
	}
	if len(candidates) == 0 {
		log.Infow("payments: no non-terminated rails to settle", "total_rails", len(rails))
		return true, nil
	}

	// Step 4: get current chain head as the cap on untilEpoch.
	head, err := t.ethClient.BlockNumber(ctx)
	if err != nil {
		return false, fmt.Errorf("payments: get chain head: %w", err)
	}
	currentEpoch := big.NewInt(int64(head))

	// Step 5: per candidate, compute the rail-specific max settlement
	// epoch via getRail() and broadcast settleRail. FilecoinPay caps
	// each rail's max settle epoch at min(settledUpTo + lockupPeriod,
	// currentChainEpoch). Passing currentEpoch naively reverts with
	// CannotSettleFutureEpochs on rails whose lockupPeriod < (current-
	// settledUpTo) gap. We do one extra getRail RPC per rail to learn
	// the safe upper bound.
	settledOK := 0
	for _, r := range candidates {
		if !stillOwned() {
			return false, nil
		}
		railView, gErr := t.payContract.GetRail(&bind.CallOpts{Context: ctx}, r.railID)
		if gErr != nil {
			log.Warnw("payments: getRail failed; skipping", "rail_id", r.railID.String(), "err", gErr)
			_, _ = t.db.ExecI(ctx,
				`UPDATE pdp_payment_rails SET last_settle_error = ? WHERE rail_id = ?`,
				gErr.Error(), r.railID.Int64())
			continue
		}
		// Eligibility heuristic, ported from upstream
		// filecoinpayment.SettleLockupPeriod: only attempt settlement
		// once at least (lockupPeriod - 1 day) epochs have accrued since
		// the rail's last on-chain settle. This avoids the
		// CannotSettleFutureEpochs revert that fires when we ask too
		// soon (the account-side lockupLastSettledAt has not yet
		// advanced).
		//
		// We DON'T attempt to model the exact contract math; we just
		// follow upstream's wait-and-batch shape. Rails accrue
		// continuously; a missed cycle costs nothing.
		const epochsInDay = 2880 // 30s blocks * 2880 = 1 day
		settleInterval := big.NewInt(epochsInDay * 7)
		// Test/diagnostic override: CURIO_CORE_PAYMENTS_MIN_SETTLE_EPOCHS
		// lets cc-smoke and operators shrink the wait-before-settle window
		// when smoke-testing or recovering from a long offline gap. Don't
		// document this for end users; it shortcircuits the upstream
		// grace logic that exists to avoid CannotSettleFutureEpochs.
		if v := os.Getenv("CURIO_CORE_PAYMENTS_MIN_SETTLE_EPOCHS"); v != "" {
			if iv, perr := strconv.ParseInt(v, 10, 64); perr == nil && iv > 0 {
				settleInterval = big.NewInt(iv)
			}
		}
		gracedLockup := new(big.Int).Sub(railView.LockupPeriod, big.NewInt(epochsInDay))
		if gracedLockup.Sign() > 0 && gracedLockup.Cmp(settleInterval) < 0 {
			settleInterval = gracedLockup
		}
		// If lockupPeriod <= 1 day, settle on every cycle (small grace).
		eligible := true
		if gracedLockup.Sign() > 0 {
			nextSettle := new(big.Int).Add(railView.SettledUpTo, settleInterval)
			if nextSettle.Cmp(currentEpoch) >= 0 {
				eligible = false
			}
		}
		if !eligible {
			log.Debugw("payments: rail not yet eligible for settlement",
				"rail_id", r.railID.String(),
				"settled_up_to", railView.SettledUpTo.String(),
				"lockup_period", railView.LockupPeriod.String(),
				"settle_interval", settleInterval.String(),
				"current_epoch", currentEpoch.String())
			continue
		}
		untilEpoch := new(big.Int).Set(currentEpoch)
		txHash, sendErr := t.settleOne(ctx, r.railID, untilEpoch)
		if sendErr != nil {
			log.Warnw("payments: settleRail send failed", "rail_id", r.railID.String(), "err", sendErr)
			_, _ = t.db.ExecI(ctx,
				`UPDATE pdp_payment_rails SET last_settle_error = ? WHERE rail_id = ?`,
				sendErr.Error(), r.railID.Int64())
			continue
		}
		log.Infow("payments: settleRail broadcast",
			"rail_id", r.railID.String(),
			"until_epoch", untilEpoch.String(),
			"tx_hash", txHash.Hex())
		_, _ = t.db.ExecI(ctx,
			`UPDATE pdp_payment_rails
			 SET last_settled_epoch  = ?,
			     last_settle_tx_hash = ?,
			     last_settle_error   = NULL,
			     last_settled_at     = datetime('now')
			 WHERE rail_id = ?`,
			untilEpoch.Int64(), txHash.Hex(), r.railID.Int64())
		_, _ = t.db.ExecI(ctx,
			`INSERT OR IGNORE INTO filecoin_payment_transactions (tx_hash, rail_ids)
			 VALUES (?, ?)`,
			txHash.Hex(), fmt.Sprintf("[%s]", r.railID.String()))
		settledOK++
	}

	log.Infow("payments: cycle complete",
		"discovered", len(rails),
		"non_terminated", len(candidates),
		"settled", settledOK,
		"current_epoch", currentEpoch.String())
	return true, nil
}

// railInfo is the local view of a single getRailsForPayeeAndToken row.
type railInfo struct {
	railID     *big.Int
	terminated bool
	endEpoch   *big.Int
}

// discover pages through getRailsForPayeeAndToken to enumerate every
// rail FilecoinPay has on file for our payee + USDFC token.
func (t *Task) discover(ctx context.Context) ([]railInfo, error) {
	opts := &bind.CallOpts{Context: ctx}
	out := make([]railInfo, 0)
	offset := big.NewInt(0)
	limit := big.NewInt(MaxRailsPerDiscovery)
	for {
		result, err := t.payContract.GetRailsForPayeeAndToken(opts, t.payee, t.usdfcAddr, offset, limit)
		if err != nil {
			return nil, fmt.Errorf("getRailsForPayeeAndToken(offset=%s): %w", offset.String(), err)
		}
		for i := range result.Results {
			r := result.Results[i]
			out = append(out, railInfo{
				railID:     new(big.Int).Set(r.RailId),
				terminated: r.IsTerminated,
				endEpoch:   new(big.Int).Set(r.EndEpoch),
			})
		}
		// nextOffset==total means we're done. Some FilecoinPay versions
		// return nextOffset=0 at end-of-iteration; treat that as done too.
		if result.NextOffset.Cmp(result.Total) >= 0 || result.NextOffset.Sign() == 0 || len(result.Results) == 0 {
			break
		}
		offset = new(big.Int).Set(result.NextOffset)
		if len(out) > MaxRailsPerDiscovery*4 {
			// Hard cap to keep us from runaway pagination on a buggy
			// contract reply. 4x the page size is plenty for V1.
			log.Warnw("payments: discovery hit hard cap, truncating",
				"seen", len(out), "cap", MaxRailsPerDiscovery*4)
			break
		}
	}
	return out, nil
}

// upsertRails INSERT-OR-UPDATEs the discovered rails into pdp_payment_rails.
// On first sight a row is inserted with default settlement fields. On
// re-sight we only bump last_seen_at and refresh terminated/end_epoch.
func (t *Task) upsertRails(ctx context.Context, rails []railInfo) error {
	for _, r := range rails {
		// Look up payer/operator from getRail for first-sight detail.
		// Cached after that; we only call getRail when the row is new
		// to keep the per-cycle RPC cost flat.
		exists, err := t.rowExists(ctx, r.railID)
		if err != nil {
			return err
		}
		terminated := 0
		if r.terminated {
			terminated = 1
		}
		if !exists {
			detail, dErr := t.payContract.GetRail(&bind.CallOpts{Context: ctx}, r.railID)
			if dErr != nil {
				log.Warnw("payments: getRail failed; inserting with empty payer/operator",
					"rail_id", r.railID.String(), "err", dErr)
				// Insert with empty strings to avoid blocking the cycle.
				_, err := t.db.ExecI(ctx,
					`INSERT INTO pdp_payment_rails
					   (rail_id, payer, payee, token, terminated, end_epoch)
					 VALUES (?, ?, ?, ?, ?, ?)`,
					r.railID.Int64(),
					"",
					strings.ToLower(t.payee.Hex()),
					strings.ToLower(t.usdfcAddr.Hex()),
					terminated,
					r.endEpoch.Int64(),
				)
				if err != nil {
					return fmt.Errorf("insert pdp_payment_rails id=%s: %w", r.railID.String(), err)
				}
				continue
			}
			_, err = t.db.ExecI(ctx,
				`INSERT INTO pdp_payment_rails
				   (rail_id, payer, payee, token, operator, validator, terminated, end_epoch)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				r.railID.Int64(),
				strings.ToLower(detail.From.Hex()),
				strings.ToLower(detail.To.Hex()),
				strings.ToLower(detail.Token.Hex()),
				strings.ToLower(detail.Operator.Hex()),
				strings.ToLower(detail.Validator.Hex()),
				terminated,
				r.endEpoch.Int64(),
			)
			if err != nil {
				return fmt.Errorf("insert pdp_payment_rails id=%s: %w", r.railID.String(), err)
			}
			log.Infow("payments: new rail discovered",
				"rail_id", r.railID.String(),
				"payer", detail.From.Hex(),
				"operator", detail.Operator.Hex(),
				"payment_rate", detail.PaymentRate.String())
			continue
		}
		// Refresh terminal-state mirror + last_seen_at.
		_, err = t.db.ExecI(ctx,
			`UPDATE pdp_payment_rails
			 SET terminated   = ?,
			     end_epoch    = ?,
			     last_seen_at = datetime('now')
			 WHERE rail_id = ?`,
			terminated, r.endEpoch.Int64(), r.railID.Int64())
		if err != nil {
			return fmt.Errorf("update pdp_payment_rails id=%s: %w", r.railID.String(), err)
		}
	}
	return nil
}

func (t *Task) rowExists(ctx context.Context, railID *big.Int) (bool, error) {
	var dummy int64
	err := t.db.QueryRowI(ctx,
		`SELECT 1 FROM pdp_payment_rails WHERE rail_id = ? LIMIT 1`,
		railID.Int64(),
	).Scan(&dummy)
	if err == nil {
		return true, nil
	}
	if isNoRows(err) {
		return false, nil
	}
	return false, fmt.Errorf("rowExists rail_id=%s: %w", railID.String(), err)
}

func isNoRows(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no rows") || strings.Contains(s, "no rows in result set")
}

// settleOne builds + broadcasts a settleRail tx via SenderETH. Returns
// the broadcasted tx hash on success.
func (t *Task) settleOne(ctx context.Context, railID, untilEpoch *big.Int) (common.Hash, error) {
	// Build the calldata via the generated binding's transactor. We
	// don't have the eth_keys private key here (SenderETH has it), so
	// the trick is: pack only, no signing/sending via bind.
	parsedABI := filecoinpayment.PaymentsMetaData
	if parsedABI == nil {
		return common.Hash{}, fmt.Errorf("filecoinpayment.PaymentsMetaData nil")
	}
	abiObj, err := parsedABI.GetAbi()
	if err != nil {
		return common.Hash{}, fmt.Errorf("get FilecoinPay ABI: %w", err)
	}
	data, err := abiObj.Pack("settleRail", railID, untilEpoch)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack settleRail(%s, %s): %w", railID.String(), untilEpoch.String(), err)
	}
	tx := types.NewTransaction(
		0, // SenderETH assigns the nonce.
		t.payContractAddr,
		big.NewInt(0), // No native value.
		0,             // SenderETH fills gas limit via simulation.
		nil,           // SenderETH fills gas price.
		data,
	)
	return t.sender.Send(ctx, t.payee, tx, "pdp-rail-settle")
}

// CanAccept accepts every offered task; the work is bounded and
// idempotent.
func (t *Task) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

// TypeDetails registers this as a singleton task that fires every
// PollInterval via IAmBored.
func (t *Task) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "PDPv0_PaySettle",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20, // ABI + bigint math; small but non-zero.
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(PollInterval, t),
	}
}

// Adder is unused; tasks are scheduled exclusively via IAmBored.
func (t *Task) Adder(taskFunc harmonytask.AddTaskFunc) {}

// Compile-time + runtime guards.
var _ harmonytask.TaskInterface = (*Task)(nil)
var _ = harmonytask.Reg(&Task{})
