# Curio Core Architecture (Alpha Distillation)

This alpha keeps strict boundaries for future hybridization:

- `internal/snapshot` → snapshot source/download/verify/cleanup
- `internal/import` → snapshot import pipeline and progress reporting
- `internal/node` → node skeleton lifecycle and datastore prep
- `internal/status` → stage/progress persistence
- `internal/config` → local deterministic config model

Design baseline:
- Lotus compatibility expectations (network semantics/RPC model)
- Venus modular service boundary style
- Forest-inspired fast bootstrap UX and sync ergonomics

This is intentionally bootstrap-focused. Consensus-critical execution remains roadmap work.
