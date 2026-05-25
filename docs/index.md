---
layout: home

hero:
  name: Curio Core
  text: One-binary Filecoin hot-storage SP.
  tagline: Pure Go. Embedded chain node. SQLite, not Yugabyte. No Lotus sidecar.
  image:
    src: /logo.svg
    alt: Curio Core
  actions:
    - theme: brand
      text: Quickstart
      link: /getting-started/quickstart
    - theme: alt
      text: Architecture
      link: /concepts/architecture
    - theme: alt
      text: GitHub →
      link: https://github.com/Reiers/curio-core

features:
  - icon: 📦
    title: A single binary
    details: ~88 MB static binary. No CGo, no Rust, no filecoin-ffi, no Lotus sidecar, no Yugabyte cluster. Drop on one VM and run.
  - icon: 🔗
    title: Embedded Lantern
    details: Pure-Go light client built into the SP. End-to-end cryptographic chain verification with no external full node.
  - icon: 💾
    title: SQLite all the way down
    details: Drop-in replacement for Yugabyte. Same harmonytask scheduler, same pdpv0 task surface, single .sqlite file you can `cp` to back up.
  - icon: 💸
    title: PDP + USDFC built in
    details: The proof loop runs autonomously. Payment rail discovery + settleRail dispatch is a singleton harmonytask. Operator only funds the wallet.
  - icon: 🖥
    title: Operator dashboard
    details: Server-rendered WebUI with chain head, dataset list, USDFC rails, scheduler health, file upload, embedded terminal.
  - icon: 🪶
    title: Minimum-viable shape
    details: The answer to "what's the smallest infra to run a paid PDP business?" Built for solo operators and laptops, not datacenters.
---

## Why Curio Core exists

Today the answer to "I want to run a paid Filecoin Onchain Cloud SP" is roughly:

```
- a 76 GB Lotus full node
- a 3-node Yugabyte cluster
- a Curio cluster
- a Boost market node
- a public ETH RPC sidecar for FEVM forwarding
- a separate IPNI announcer
- a dashboard, a wallet manager, a settlement watcher, monitoring...
```

Curio Core's answer is **one binary**. Drop it on a single VM, point a domain at it, fund a wallet. You're a Filecoin Onchain Cloud hot-storage provider.

## Status

Pre-alpha. The proof loop ships on Filecoin Calibration today. Mainnet readiness is the
Q3 milestone. See the [status & roadmap](/status) page and
[curio-core#10](https://github.com/Reiers/curio-core/issues/10) for current health.

## Where to start

- **Run it** in 5 minutes → [Quickstart](/getting-started/quickstart)
- **Understand it** → [Architecture](/concepts/architecture)
- **Operate it** → [Dashboard tour](/operating/dashboard)
