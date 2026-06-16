# Contributing

Curio Core is operated by [Reiers](https://github.com/Reiers) (TSE Reiersen) and lives
at <https://github.com/Reiers/curio-core>.

## Reporting issues

<https://github.com/Reiers/curio-core/issues>

Helpful issue content:

- `curio-core version`
- Whether you're on calibration or mainnet
- The error or symptom
- Last ~10 minutes of `sudo journalctl -u curio-core` (lightly redacted)
- Whether it reproduces or was a one-off

## Pull requests

Pre-alpha. PRs are welcome but the surface is shifting fast. Before significant work,
open a discussion issue first so we can confirm the direction.

For small fixes (typos, doc clarifications, low-risk patches), just open the PR.

## Development setup

```bash
git clone https://github.com/Reiers/curio-core
cd curio-core

# Build the binary
CGO_ENABLED=0 go build -o curio-core ./cmd/curio-core

# Run on calibration with a temp data dir
./curio-core run --data-dir /tmp/curio-core-dev --network calibration --listen 127.0.0.1:4711
```

The Go module pulls in our patched `Reiers/curio` fork via a `replace` directive in
`go.mod`. Real local development against a checked-out curio fork requires another
`replace` pointing at the local path:

```
// go.mod
replace github.com/filecoin-project/curio => ../curio
```

Don't forget to revert that before pushing.

## Testing

```bash
go test ./...
```

Most tests in `internal/` are unit tests with no external deps. The integration tests
under `internal/synapsecompat/` require a running daemon — see the test file headers.

## Project structure

See [Architecture](/concepts/architecture#code-map) for the package map.

## Code style

Standard Go: `gofmt`, `go vet`, comments on exported APIs, errors wrapped with
`fmt.Errorf("...: %w", err)`. We don't run a heavy linter in CI; just keep the file
readable.

For SQL embedded in Go strings, prefer raw string literals for readability:

```go
err := db.SelectI(ctx, &rows, `
    SELECT id, name
    FROM harmony_task
    WHERE owner_id IS NULL
    ORDER BY id ASC
`)
```

## License

Apache 2.0 OR MIT, dual-licensed. By contributing you agree your contribution is
licensed under both.
