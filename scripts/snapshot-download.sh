#!/usr/bin/env bash
set -euo pipefail

NETWORK="${1:-mainnet}"
OUTDIR="${2:-$HOME/.curio/snapshots/$NETWORK}"

case "$NETWORK" in
  mainnet) URL="https://forest-archive.chainsafe.dev/latest/mainnet/" ;;
  calibnet) URL="https://forest-archive.chainsafe.dev/latest/calibnet/" ;;
  *) echo "network must be mainnet or calibnet" >&2; exit 1 ;;
esac

mkdir -p "$OUTDIR"
aria2c -x5 --summary-interval=1 --console-log-level=notice -d "$OUTDIR" -o "$NETWORK-latest.car.zst" "$URL"
