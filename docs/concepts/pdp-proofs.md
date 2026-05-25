# PDP proof loop

**PDP** (Proof of Data Possession) is Filecoin's continuous-proof storage protocol.
The SP commits piece data once, then re-proves possession on a regular cadence
(typically every ~2 hours per dataset) to claim the next chunk of accrued USDFC.

## How the loop runs in curio-core

Three harmonytask types handle the cycle:

| Task | What it does | Cadence |
|---|---|---|
| `PDPv0_Prove` | Reads `pdp_data_sets.prove_at_epoch`, computes the SHA-256 Merkle proofs for the challenged leaves, signs + broadcasts the `provePossession` tx via SenderETH. | Once per dataset per proving window (~every 2h) |
| `PDPv0_InitPP` | After a successful prove, calls `nextProvingPeriod()` on the contract to commit the next window's challenge seed. | Triggered per dataset per prove |
| `PDPv0_SaveCache` | Pre-computes Merkle trees so the next prove cycle reads cached state instead of recomputing from raw piece bytes. | Opportunistic |

The proving cadence is determined by the on-chain `proving_period` (calibration default:
~2880 epochs = ~24 hours; tunable per dataset). Each prove fires at
`prove_at_epoch ± challenge_window`.

## What does a prove transaction look like?

```
provePossession(
    setId:    <dataset id>,
    proofs:   <array of (leaf, merkle_proof) tuples>
) external payable
```

The contract verifies each `(leaf, merkle_proof)` against the Merkle root committed
when pieces were added to the dataset, against the challenge seed derived from the
DRAND beacon at the prove window's start. If all challenges verify, the dataset's
`settledUpTo` advances and the rail accumulator credits the SP.

## Retry budget

The `PDPv0_Prove` task has `MaxFailures=10` with a 30-second backoff between retries.
That gives a **25-minute total retry budget** per dataset per window. Past that the
task gives up and the dataset's `consecutive_prove_failures` counter increments.

After 5 consecutive failures the dataset is at risk of `unrecoverable_proving_failure`
which triggers FWSS service termination on-chain.

## Reading proof health

The dashboard's **Tasks** page lists recent `PDPv0_Prove` entries with their `result`
(0 = fail, 1 = ok). The **Datasets** page shows per-dataset `consecutive_prove_failures`.

For programmatic monitoring:

```sql
-- last 24h prove success rate
SELECT
  COUNT(*) FILTER (WHERE result = 1) AS ok,
  COUNT(*) FILTER (WHERE result = 0) AS fail
FROM harmony_task_history
WHERE name = 'PDPv0_Prove'
  AND work_end >= datetime('now', '-24 hours');
```

## Common failure modes

| Symptom | Likely cause | Action |
|---|---|---|
| `failed to get chain randomness` | Lantern head is behind real chain | Wait; usually resolves within minutes. If chronic, see [troubleshooting](/help/troubleshooting) |
| `failed to submit possession proof: insufficient funds` | PDP wallet out of gas | Top up tFIL/FIL on the wallet |
| `failed to generate proof: no subpiece found` | Piece bytes missing from stash | A piece's stash file was deleted/corrupted. Out-of-band restore from backup. |
| Task sits at `at max already` | Resource budget too tight | Bump `--engine-cpu` / `--engine-ram` flags (defaults are 4 CPU / 4 GiB) |
| `CannotSettleFutureEpochs` on settle | Account-level lockup not advanced yet | Expected — settles run on a 7-day grace cycle |
