# Curio Core sprint plan

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11)
Branch cut: 2026-05-23

## Day 1 — Recon

- [ ] Clone `filecoin-project/curio@integ/task` locally
- [ ] Walk the task graph from `cmd/curio/tasks/tasks.go`, identify every package `tasks/pdp` + `tasks/pdpv0` depend on (transitively)
- [ ] Walk `harmony/harmonydb/sql/` and classify each migration as PDP-relevant or sealing-only
- [ ] Produce `docs/SCOPE-DIFF.md` with the concrete in/out list
- [ ] Confirm `integ/task` SHA to pin against (current head or wait for Andy tag)

## Day 2-3 — Bones

- [ ] Add Lantern as a Go module dependency (requires Lantern A1: `lantern.NewDaemon` export)
- [ ] Add Curio integ/task as a `replace`-pinned dep
- [ ] Get `go build ./...` clean with just Lantern + PDP tasks compiled in
- [ ] Stub the DB driver with a noop implementation so it compiles
- [ ] First boot test: process starts, Lantern reaches the network, no panics

## Day 4-5 — SQLite port

- [ ] Add `modernc.org/sqlite` driver
- [ ] Port Postgres → SQLite for the PDP-relevant migrations (~15-25 files)
- [ ] Adapt `UPDATE ... SKIP LOCKED` semantics via `BEGIN IMMEDIATE` + appropriate retry
- [ ] Boot test: tasks register, claim, complete

## Day 6-7 — Tests

- [ ] Port Curio's PDP test suite (tasks/pdp/*_test.go + tasks/pdpv0/*_test.go)
- [ ] Fix what breaks under SQLite + Lantern
- [ ] Add Curio Core's own integration test: full PDP cycle against calibration

## Day 8 — Demo

- [ ] Calibration mainnet run, observe a real PDP cycle
- [ ] Filecoin mainnet run with a real (low-stakes) miner ID
- [ ] Progress comment on Reiers/lantern#11

## Open questions for Andy

1. Pin `integ/task` against current head, or wait for a tag?
2. Miner ID generation via `StateCreateMiner` at first-run, or bring-your-own-miner-ID for v1?
3. Curio Core repo location: confirmed as standalone `Reiers/curio-core` (this repo) rather than a `Reiers/lantern:curio-core` branch — preserves Lantern's identity as a general-purpose node.

## Lantern-side prerequisite work

These items live in the Lantern repo, not here. They unblock Curio Core's day 2.

- A1: Export `lantern.NewDaemon(ctx, cfg) *Daemon` from `cmd/lantern` so we can embed it as a library instead of fork+exec.
- A2: Expose header-store + cache size as config fields with PDP-tight defaults (~50 MB ceiling).
- A3: F3 anchor refresh must tolerate the self-bridge case (the daemon IS the upstream when running under Curio Core).
