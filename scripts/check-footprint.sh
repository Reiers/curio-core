#!/usr/bin/env bash
# check-footprint.sh — enforce curio-core's footprint discipline.
#
# Per #14/#53: the linux/amd64 binary must stay under 90 MB. Any PR
# that pushes it over needs to either trim a transitive dep or get
# explicit approval (and bump the limit in this script).
#
# Usage:
#   bash scripts/check-footprint.sh
#
# Exit codes:
#   0  -> under budget
#   1  -> over budget (build still succeeds; CI should fail)
#   2  -> couldn't build (unrelated failure)

set -u

LIMIT_BYTES=$((90 * 1024 * 1024))  # 90 MB hard limit
TARGET_BYTES=$((50 * 1024 * 1024)) # 50 MB nice-to-have target

cd "$(dirname "$0")/.." || exit 2

# Build linux/amd64, pure Go, into a tmp path.
TMPBIN=$(mktemp -t curio-core-footprint.XXXXXX)
trap 'rm -f "$TMPBIN"' EXIT

echo "Building linux/amd64 (CGO_ENABLED=0)..."
if ! CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$TMPBIN" ./cmd/curio-core 2>&1; then
  echo "FAIL: build failed (unrelated to footprint)"
  exit 2
fi

# Stat-based size, portable across mac + linux.
SIZE=$(wc -c <"$TMPBIN" | tr -d ' ')

echo "Binary size: $SIZE bytes ($((SIZE / 1024 / 1024)) MB)"
echo "Target:      $TARGET_BYTES bytes ($((TARGET_BYTES / 1024 / 1024)) MB)"
echo "Hard limit:  $LIMIT_BYTES bytes ($((LIMIT_BYTES / 1024 / 1024)) MB)"

if [ "$SIZE" -gt "$LIMIT_BYTES" ]; then
  echo
  echo "FAIL: binary exceeds the 90 MB hard limit by $(( (SIZE - LIMIT_BYTES) / 1024 / 1024 )) MB"
  echo "See curio-core#53 for the discipline rationale. Options:"
  echo "  1. Trim a transitive dep (run 'go mod why <pkg>' to chase the offender)"
  echo "  2. Bump LIMIT_BYTES here with explicit reasoning in the PR"
  exit 1
fi

if [ "$SIZE" -gt "$TARGET_BYTES" ]; then
  echo
  echo "OK (over soft target): $(( (SIZE - TARGET_BYTES) / 1024 / 1024 )) MB above the 50 MB target."
  echo "This is acceptable; consider trimming on the next major refactor."
else
  echo
  echo "OK: under the 50 MB soft target."
fi
exit 0
