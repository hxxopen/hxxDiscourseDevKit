#!/usr/bin/env bash
#
# Build the embedded management UI and the DevKit binaries.
#
# Environment variables:
#   VERSION    semantic version to inject (default: 0.1.0-dev)
#   OUT        output directory (default: <root>/src/bin)
#   SKIP_WEB   set to 1 to reuse the existing embedded UI build
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/src"
WEB="$SRC/web"
OUT="${OUT:-$SRC/bin}"
VERSION="${VERSION:-0.1.0-dev}"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ "${SKIP_WEB:-0}" != "1" ]]; then
  if [[ ! -d "$WEB/node_modules" ]]; then
    if [[ -f "$WEB/package-lock.json" ]]; then
      (cd "$WEB" && npm ci --no-audit --no-fund)
    else
      (cd "$WEB" && npm install --no-audit --no-fund)
    fi
  fi
  (cd "$WEB" && npm run build)
fi

GOOS_VALUE="${GOOS:-$(go env GOOS)}"
EXT=""
if [[ "$GOOS_VALUE" == "windows" ]]; then
  EXT=".exe"
fi

mkdir -p "$OUT"
cd "$SRC"

PKG="github.com/hxx/hxxDiscourseDevKit/internal/version"
LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X $PKG.Version=$VERSION"
LDFLAGS="$LDFLAGS -X $PKG.Commit=$COMMIT"
LDFLAGS="$LDFLAGS -X $PKG.BuildDate=$BUILD_DATE"

echo "Building $VERSION ($COMMIT, $BUILD_DATE) for $GOOS_VALUE"
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/hxxddk$EXT" ./cmd/hxxddk
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/devkitd$EXT" ./cmd/devkitd

echo "Binaries:"
echo "  $OUT/hxxddk$EXT"
echo "  $OUT/devkitd$EXT"
