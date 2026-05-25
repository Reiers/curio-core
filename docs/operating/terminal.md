# Embedded terminal

The dashboard ships with an **in-browser terminal** at `/terminal`. It is **not** a
full shell — only a fixed allowlist of read-only curio-core subcommands can be invoked.

## Why an allowlist

A real in-browser shell is a massive auth/RCE surface. Curio Core is loopback-only
today with no auth layer; anyone with network access to port 14994 effectively owns
the box if they can run arbitrary commands. The allowlist is the security model.

For mutations (`wallet new`, `wallet send`, `demo *`, `sp register`), SSH into the host
and run the CLI directly.

## What's in the allowlist

| Command | Purpose |
|---|---|
| `version` | Print the curio-core build version |
| `wallet list` | List all `eth_keys` rows with role + tFIL/USDFC balances |
| `doctor` | Read-only health + DB ↔ chain reconciliation report |
| `sp info` | Show this SP's Service Registry registration (if registered) |
| `probe` | Smoke-test the embedded Lantern daemon's anchoring |
| `config show` | Print the current `harmony_config` rows |

All are **read-only** and idempotent. Running any of them never changes state.

## How requests are guarded

When the terminal POSTs to `/api/run`:

1. Body is JSON: `{ "args": ["wallet", "list"] }`.
2. Each arg is checked against shell metacharacters — anything containing
   `` `$|&;<>()\\"'\n `` is rejected immediately. Curio Core CLI vocabulary never
   needs these, so this catches escape attempts.
3. The argv is matched against the allowlist as a **prefix**: `["wallet", "list"]`
   passes; `["wallet", "new"]` does not.
4. The current `curio-core` binary is resolved via `os.Executable()` (no PATH lookup),
   `exec.Command`-ed with the validated argv, captured stdout/stderr capped at 64 KiB,
   and a 30-second hard timeout.
5. Response: `{ ok, exit_code, stdout, stderr, duration }`.

## Keyboard shortcuts

| Key | Action |
|---|---|
| `Enter` | Run command |
| `↑` | Recall previous command |
| `↓` | Recall next command (or clear) |

Clickable chips below the terminal run each allowlisted command with one click.

## Extending the allowlist

The allowlist lives in `internal/dashboard/runner.go`:

```go
func allowlistedSubcommands() []string {
    return []string{
        "version",
        "wallet list",
        "doctor",
        "sp info",
        "probe",
        "config show",
    }
}
```

To add a new command, it must be:

1. **Read-only** — no DB mutations, no on-chain broadcasts.
2. **Bounded** — finishes in well under 30 seconds.
3. **Quiet on stdout** — doesn't paginate, doesn't expect a TTY.

A `config get <key>` accessor is the most likely next addition. Anything that mutates
should stay on the SSH side.
