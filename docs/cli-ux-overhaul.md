# CLI UX Overhaul (Next Alpha Groundwork)

## Objectives
- Keep existing alpha paths intact.
- Add clear staged outputs and practical examples.
- Add `--explain` mode for key workflows (`sync`, `doctor`, `wallet new`, `chain msg`).

## New/Extended Commands
- `curio doctor [--data-dir <path>] [--json] [--explain]`
- `curio chain msg --decode <hex|base64> [--explain]`
- `curio chain coverage-report`
- `curio wallet new|list|show|export|import|resolve|sign|verify`

## UX Requirements
1. Command help text includes short examples.
2. Multi-step commands print stage markers (`[1/2]`, etc.) where meaningful.
3. Failed checks/errors include concrete remediation instructions.
4. `--explain` text explains why each step exists, not just what it does.

## Doctor Output Contract
Checks:
- `aria2c-installed`
- `disk-space` on `~/.curio`
- `data-dir-writable`

Each failed check must include `fix:` line with actionable next step.

## Acceptance Criteria (Measurable)
1. `curio doctor` exits non-zero on any failed check and exits 0 when all pass.
2. `curio doctor --json` outputs parseable JSON array of checks.
3. `curio sync --explain` prints stage explanation before execution.
4. `curio wallet new --explain` prints alpha security note.
5. `curio chain msg --decode ... --explain` prints decode strategy.
