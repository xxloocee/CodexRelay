#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${VERSION:?VERSION is required}"
ARCH="${ARCH:?ARCH is required (amd64 or arm64)}"

VERSION="${VERSION#v}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  echo "Invalid SemVer: $VERSION" >&2
  exit 1
fi
BUILD_VERSION="${VERSION%%[-+]*}"

case "$ARCH" in
  amd64) GO_ARCH="amd64" ;;
  arm64) GO_ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

DISPLAY_ARCH="$ARCH"
if [[ "$ARCH" == "amd64" ]]; then
  DISPLAY_ARCH="x64"
fi

DIST_DIR="$PROJECT_ROOT/dist"
MACOS_BUILD_DIR="$PROJECT_ROOT/build/macos"
APP_DIR="$DIST_DIR/CodexRelay.app"
BINARY="$DIST_DIR/CodexRelay-$VERSION-$ARCH"
DMG="$DIST_DIR/CodexRelay-$VERSION-macos-$DISPLAY_ARCH.dmg"
ICON="$MACOS_BUILD_DIR/icon.icns"

mkdir -p "$DIST_DIR" "$MACOS_BUILD_DIR"
rm -rf "$APP_DIR"
rm -f "$BINARY" "$DMG"

case "$(uname -m)" in
  x86_64) HOST_GOARCH="amd64" ;;
  arm64|aarch64) HOST_GOARCH="arm64" ;;
  *) echo "Unsupported runner architecture: $(uname -m)" >&2; exit 1 ;;
esac

# Wails code generation must run using the runner's own architecture.
export GOOS=darwin
export GOARCH="$HOST_GOARCH"
export CGO_ENABLED=1
export GOTELEMETRY=off

WAILS="github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.11"
cd "$PROJECT_ROOT"
go run "$WAILS" generate icons -input "$PROJECT_ROOT/logo.png" -macfilename "$ICON"
go run "$WAILS" generate bindings -b -d "$PROJECT_ROOT/frontend/bindings" -clean ./internal/desktop
if [[ "$HOST_GOARCH" == "$GO_ARCH" ]]; then
  go test ./...
  go vet ./...
fi
while IFS= read -r -d '' script; do
  node --check "$script"
done < <(find "$PROJECT_ROOT/frontend" -type f -name '*.js' ! -path '*/bindings/*' ! -path '*/vendor/*' -print0)
export GOARCH="$GO_ARCH"
go build -trimpath -tags production -ldflags "-s -w -X codexrelay/internal/desktop.applicationVersion=$VERSION" -o "$BINARY" ./cmd

mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
cp "$BINARY" "$APP_DIR/Contents/MacOS/CodexRelay"
cp "$ICON" "$APP_DIR/Contents/Resources/icon.icns"
sed "s/@VERSION@/$VERSION/g" "$MACOS_BUILD_DIR/Info.plist" > "$APP_DIR/Contents/Info.plist"
sed -i '' "s/@BUILD_VERSION@/$BUILD_VERSION/g" "$APP_DIR/Contents/Info.plist"
chmod +x "$APP_DIR/Contents/MacOS/CodexRelay"

if [[ -n "${MACOS_SIGNING_IDENTITY:-}" ]]; then
  codesign --force --deep --options runtime --timestamp --sign "$MACOS_SIGNING_IDENTITY" "$APP_DIR"
fi

hdiutil create -volname "CodexRelay $VERSION ($ARCH)" -srcfolder "$APP_DIR" -ov -format UDZO "$DMG"
echo "Built: $APP_DIR"
echo "Packaged: $DMG"
