# Curio Core sprint plan

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11)
Branch cut: 2026-05-23

> Live status of every day is in [`STATUS.md`](./STATUS.md). This file holds
> the planned shape of the sprint. They stay in sync.

## Day 1 — Recon

- [x] Walk `cmd/curio/tasks/tasks.go`, identify transitive deps for `tasks/pdp` + `tasks/pdpv0`
- [x] Walk `harmony/harmonydb/sql/` and classify each migration as PDP-relevant or sealing-only
- [x] Produce `docs/SCOPE-DIFF.md` with the concrete in/out list
- [x] Confirm `integ/task` SHA to pin against — `21531097` (May 22 `build ctx` head)

## Day 2 — Bones

- [x] Add Lantern as a Go module dependency (lantern A1: `lantern.NewDaemon` exported, commit 9177a99)
- [x] Define `PieceReader` interface (`internal/pieceio`) to dodge CGo on `tasks/pdp`
- [x] Get a `curio-core probe` command that boots an embedded Lantern + anchors mainnet

## Day 3 — SQL classify + port + WebUI strip

- [x] Classify all 118 upstream Curio migration files (`docs/SQL-CLASSIFICATION.md`)
- [x] Hand-translate the KEEP set → 14 SQLite files at `internal/harmonysqlite/schema-curio-core/`
- [x] `harmonysqlite.New()` applies migrations at boot, idempotency tracked in `harmony_schema_migrations`
- [x] Acceptance: `go test ./internal/harmonysqlite/...` green (8 tests + ~50 subtests, includes PDP-refcount trigger acceptance)
- [x] Vendor Curio WebUI; strip porep/clustering/sealing panels (`docs/WEBUI-STRIP-NOTES.md`)
- [x] `CGO_ENABLED=0 go build ./...` stays green

## Day 4 — Pure-Go `tasks/pdp` carve-out

- [x] Reiers/curio fork: `tasks/pdp` compiles under `CGO_ENABLED=0` via the `PieceReader` interface (a1a4449)
- [x] `internal/pdptests` scaffolded (compile-only + error-detection tests, gated behind `pdp_full_carveout` tag)

## Day 5 — Wire-up + first-run config

- [ ] Wire `harmonytask.Engine` against `harmonysqlite` (skip Peering layer; single-server)
- [ ] Register `tasks/pdp` + `tasks/pdpv0` taskTypes against the engine
- [ ] First-run config: detect missing default-layer fields, return `/setup` redirect from WebUI, block on submit
- [ ] CLI: `curio-core run --print-setup-url` on boot

## Day 6 — PDP test port

- [ ] Carve out the remaining CGo transitives (`lotus/storage/paths`, `gosigar`) the same way we did `lib/ffi`
- [ ] Drop the `pdp_full_carveout` build tag — tests run under default invocation
- [ ] Acceptance: `go test ./internal/pdptests/...` green under `CGO_ENABLED=0`

## Day 7 — Integration test

- [ ] Stand up a calibration miner (Reiers/curio fork) and wire it to Lantern as primary chain backend
- [ ] Run one full PDP cycle (deal accept → piece park → proof submit) under curio-core
- [ ] Capture logs + tipset progression as evidence

## Day 8 — Demo

- [ ] Calibration mainnet run, observe a real PDP cycle end-to-end
- [ ] Filecoin mainnet probe with a real (low-stakes) miner ID — read-only, no on-chain writes
- [ ] Progress comment on Reiers/lantern#11 with the demo trace

## Open questions for Andy (live in issue #11)

1. `integ/task` SHA pin against `21531097` — confirm stable?
2. Miner ID at first-run: bring-your-own for v1, generation via `StateCreateMiner` as v2?
3. Repo location: Curio Core as `Reiers/curio-core` (private) vs `curio-core` branch on Lantern?
4. `pages/proofshare/`: delete the dir, or stub the API to "not applicable"?
5. `win-stats.mjs` widget: drop?
6. `//go:build cgo` tag on `web/`: follow-up issue, or fold into existing CGo carve-out work?
7. WebUI's `pages/market/`, `pages/mk12-deal/`, `pages/mk12-deals/`: keep or drop?
8. First-run wizard shape: WebUI `/setup` redirect — TUI fallback for headless installs?

## Lantern-side prerequisites

These items live in the Lantern repo, not here. They unblock Curio Core's later days.

- [x] A1: `lantern.NewDaemon(ctx, cfg) *Daemon` exported (9177a99 in Reiers/lantern)
- [ ] A2: Expose header-store + cache size as config fields with PDP-tight defaults (~50 MB ceiling)
- [ ] A3: F3 anchor refresh must tolerate the self-bridge case (the daemon IS the upstream when running under Curio Core)
