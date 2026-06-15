#!/usr/bin/env bash
# Build a macOS .pkg installer for curio-core (curio-core#80 / #68).
#
# Must run on macOS (uses pkgbuild + productbuild). Builds a CGO-free
# darwin binary for the host arch (or the arch given), stages a payload
# rooted at /, and emits a component .pkg wrapped in a distribution .pkg.
#
# Usage:  packaging/build-pkg-macos.sh <version> [arch]
#   version : e.g. v0.1.0 (baked into the binary)
#   arch    : arm64 (default) | amd64
#
# The .pkg is UNSIGNED. postinstall strips the quarantine xattr so
# Gatekeeper allows the unsigned pre-alpha binary. Signing + notarization
# (Developer ID) is a separate cost decision tracked in #80.
set -euo pipefail

cd "$(dirname "$0")/.."   # repo root

[ "$(uname -s)" = "Darwin" ] || { echo "must run on macOS" >&2; exit 1; }
command -v pkgbuild >/dev/null || { echo "pkgbuild not found (install Xcode CLT)" >&2; exit 1; }

VERSION="${1:?usage: build-pkg-macos.sh <version> [arch]}"
ARCH="${2:-arm64}"
CC_VERSION="${VERSION#v}"
IDENT="io.reiers.curio-core"

mkdir -p dist
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "==> building darwin/${ARCH} binary (${VERSION})"
BIN="dist/curio-core-darwin-${ARCH}"
CGO_ENABLED=0 GOOS=darwin GOARCH="${ARCH}" go build \
    -trimpath \
    -ldflags "-s -w -X main.versionTag=${VERSION}" \
    -o "${BIN}" \
    ./cmd/curio-core
shasum -a 256 "${BIN}" | tee "${BIN}.sha256"

# Stage payload: /usr/local/bin/curio-core
ROOT="${STAGE}/root"
mkdir -p "${ROOT}/usr/local/bin"
cp "${BIN}" "${ROOT}/usr/local/bin/curio-core"
chmod 0755 "${ROOT}/usr/local/bin/curio-core"

# launchd plist -> /Library/LaunchDaemons
mkdir -p "${ROOT}/Library/LaunchDaemons"
cp "packaging/macos/${IDENT}.plist" "${ROOT}/Library/LaunchDaemons/${IDENT}.plist"
chmod 0644 "${ROOT}/Library/LaunchDaemons/${IDENT}.plist"

# Scripts dir (postinstall)
SCRIPTS="${STAGE}/scripts"
mkdir -p "${SCRIPTS}"
cp packaging/macos/scripts/postinstall "${SCRIPTS}/postinstall"
chmod 0755 "${SCRIPTS}/postinstall"

COMPONENT="${STAGE}/curio-core-component.pkg"
OUT="dist/curio-core-${CC_VERSION}-${ARCH}.pkg"

echo "==> pkgbuild component"
pkgbuild \
    --root "${ROOT}" \
    --scripts "${SCRIPTS}" \
    --identifier "${IDENT}" \
    --version "${CC_VERSION}" \
    --install-location "/" \
    "${COMPONENT}"

echo "==> productbuild distribution"
productbuild \
    --identifier "${IDENT}.dist" \
    --version "${CC_VERSION}" \
    --package "${COMPONENT}" \
    "${OUT}"

shasum -a 256 "${OUT}" | tee "${OUT}.sha256"
echo
echo "built ${OUT}"
echo "(unsigned; postinstall strips quarantine. Sign+notarize later: #80)"
