# Wallet Subsystem Plan (Alpha Scaffold)

## Goals
Introduce encrypted-at-rest local wallet storage and user-friendly CLI scaffolding while preserving alpha command stability.

## Storage Plan
- File: `~/.curio/wallet.json.enc`
- Format: JSON payload encrypted with AES-GCM using passphrase-derived key.
- Password prompt required on: `new`, `import`, `list`, `show`, `resolve`, `sign`.

## Supported Key Types
- `secp` -> placeholder `f1...` addresses
- `bls` -> placeholder `f3...` addresses
- `delegated` -> placeholder `f4...` addresses

## Commands (Scaffold)
- `curio wallet new --name <name> --type <secp|bls|delegated> [--explain]`
- `curio wallet list`
- `curio wallet show --wallet <name|address>`
- `curio wallet export` (stub)
- `curio wallet import --name <name> --type <...> --private-key <value>`
- `curio wallet resolve --address <f...>`
- `curio wallet sign --wallet <name|address> --message <text>`
- `curio wallet verify --signature <value>`

## f2 Resolution Note
`wallet resolve` prints a TODO for `f2` until chain lookup integration lands.

## Alpha Security Limitations
- Passphrase derivation is intentionally simple (not production-hard KDF).
- Placeholder key generation/signature behavior is for UX and flow validation only.
- Do not use for production-value wallets.

## Acceptance Criteria (Measurable)
1. Creating wallet writes encrypted file at `~/.curio/wallet.json.enc`.
2. Listing/showing wallets with wrong password fails with decrypt error.
3. New wallet address prefixes match key type mapping (`f1/f3/f4`).
4. `wallet resolve --address f2...` prints explicit TODO chain-lookup message.
5. `wallet sign` emits deterministic alpha signature payload format.
