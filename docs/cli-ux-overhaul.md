# CLI UX Overhaul (Next Alpha Groundwork)

## Objectives
- Keep existing alpha paths intact.
- Add clear staged outputs and practical examples.
- Add `--explain` mode for key workflows (`sync`, `doctor`, `wallet new`, `chain msg`).

## New/Extended Commands
- `curiocore doctor [--data-dir <path>] [--json] [--explain]`
- `curiocore chain msg --decode <hex|base64> [--explain]`
- `curiocore chain coverage-report`
- `curiocore wallet new|list|show|export|import|resolve|sign|verify`

## UX Requirements
1. Command help text includes short examples.
2. Multi-step commands print stage markers (`[1/2]`, etc.) where meaningful.
3. Failed checks/errors include concrete remediation instructions.
4. `--explain` text explains why each step exists, not just what it does.

## Doctor Output Contract
Checks:
- `aria2c-installed`
- `disk-space` on `~/.curiocore`
- `data-dir-writable`

Each failed check must include `fix:` line with actionable next step.

## Acceptance Criteria (Measurable)
1. `curiocore doctor` exits non-zero on any failed check and exits 0 when all pass.
2. `curiocore doctor --json` outputs parseable JSON array of checks.
3. `curiocore sync --explain` prints stage explanation before execution.
4. `curiocore wallet new --explain` prints alpha security note.
5. `curiocore chain msg --decode ... --explain` prints decode strategy.
