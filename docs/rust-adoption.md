# Curio Core Rust Adoption Decision

Date: 2026-02-13

## Decision

**Choose Option A: Rust sidecar service (recommended), but only for data-plane heavy paths first.**

This is the lowest-risk path that gives real performance upside without destabilizing consensus-critical behavior.

## Where Rust goes first

1. **Snapshot import + CAR parsing**
   - High CPU/IO pressure
   - Easy to validate with deterministic outputs
2. **AMT/HAMT traversal and state decode acceleration**
   - Hot loops suitable for Rust memory/perf profile
3. **Block validation acceleration helpers (non-authoritative initially)**
   - Run as assistive path with parity checks against Go path

## What stays Go now (Lotus parity critical)

- Fork choice and canonical head decision
- Reorg application semantics
- Network-version/upgrade policy wiring
- RPC compatibility surface and operational controls
- Any authoritative consensus decision until conformance harness is mature

## Safest boundary choice

**Boundary: sidecar over gRPC/IPC**

Why:
- Clear process isolation and crash containment
- Versioned contracts and easier rollout/rollback
- Better observability than raw FFI
- Avoids GC/ownership complexity and CGO brittleness in consensus hot path

## What we explicitly avoid right now

- No fragile CGO/FFI in the core sync decision loop
- No language-percentage cargo-cult migration
- No consensus-authoritative Rust execution cutover before differential parity proof

## Rollout policy

- Start Rust sidecar in shadow/assist mode
- Compare outputs against Go path
- Promote functionality only after stable no-diff windows and replay tests
