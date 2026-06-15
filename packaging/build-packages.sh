#!/usr/bin/env bash
# Build curio-core Linux .deb/.rpm packages for amd64 + arm64.
#
# Usage:  packaging/build-packages.sh <version> [arch...]
#   version : e.g. v0.1.0  (also baked into the binary via -ldflags)
#   arch    : amd64 arm64  (default: both)
#
# Requires: nfpm on PATH (go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest).
# Produces: dist/curio-core-linux-<arch>            (raw binary + .sha256)
#           dist/curio-core_<ver>_<arch>.deb
#           dist/curio-core-<ver>.<arch>.rpm
#
# macOS .pkg is built by packaging/build-pkg-macos.sh (runs on macOS only).
# curio-core now builds CGO-free on darwin via the third_party/gosigar
# replace (#80), so darwin is a first-class target.
set -euo pipefail

cd "$(dirname "$0")/.."   # repo root

VERSION="${1:?usage: build-packages.sh <version> [arch...]}"
shift || true
ARCHES=("$@")
if [ ${#ARCHES[@]} -eq 0 ]; then
    ARCHES=(amd64 arm64)
fi

command -v nfpm >/dev/null 2>&1 || { echo "nfpm not on PATH" >&2; exit 1; }

mkdir -p dist
# nfpm wants a bare version (strip leading v) for clean .deb/.rpm versions.
CC_VERSION="${VERSION#v}"

for ARCH in "${ARCHES[@]}"; do
    echo "==> building linux/${ARCH} binary (${VERSION})"
    OUT="dist/curio-core-linux-${ARCH}"
    CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build \
        -trimpath \
        -ldflags "-s -w -X main.versionTag=${VERSION}" \
        -o "${OUT}" \
        ./cmd/curio-core
    sha256sum "${OUT}" > "${OUT}.sha256" 2>/dev/null || shasum -a 256 "${OUT}" > "${OUT}.sha256"
    echo "    $(cat "${OUT}.sha256")"

    # nfpm does NOT env-expand contents[].src, so render a concrete config
    # per arch with envsubst (expands ${CC_VERSION} ${CC_ARCH} ${CC_BINARY}).
    export CC_VERSION CC_ARCH="${ARCH}" CC_BINARY="${OUT}"
    RENDERED="dist/nfpm-${ARCH}.yaml"
    envsubst '${CC_VERSION} ${CC_ARCH} ${CC_BINARY}' < packaging/nfpm.yaml > "${RENDERED}"
    echo "==> packaging .deb + .rpm (${ARCH})"
    nfpm pkg --packager deb --config "${RENDERED}" --target dist/
    nfpm pkg --packager rpm --config "${RENDERED}" --target dist/
    rm -f "${RENDERED}"
done

echo
echo "Artifacts in dist/:"
ls -1 dist/ | sed 's/^/  /'
