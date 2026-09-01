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

case "$ARCH" in
  amd64) EXPECTED_HOST_ARCH="amd64" ;;
  arm64) EXPECTED_HOST_ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64) HOST_ARCH="amd64" ;;
  aarch64|arm64) HOST_ARCH="arm64" ;;
  *) echo "Unsupported runner architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [[ "$HOST_ARCH" != "$EXPECTED_HOST_ARCH" ]]; then
  echo "Linux packages must be built on a native $ARCH runner; got $HOST_ARCH" >&2
  exit 1
fi

DIST_DIR="$PROJECT_ROOT/dist"
PACKAGE_ROOT="$PROJECT_ROOT/build/linux/package-root"
BINARY="$PACKAGE_ROOT/usr/bin/codexrelay"
DEB="$DIST_DIR/CodexRelay-$VERSION-linux-$ARCH.deb"
WAILS="github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.11"

mkdir -p "$DIST_DIR"
rm -rf "$PACKAGE_ROOT"
rm -f "$DEB"

export GOOS=linux
export GOARCH="$ARCH"
export CGO_ENABLED=1
export GOTELEMETRY=off

cd "$PROJECT_ROOT"
go run "$WAILS" generate bindings -b -d "$PROJECT_ROOT/frontend/bindings" -clean ./internal/desktop

while IFS= read -r -d '' script; do
  node --check "$script"
done < <(find "$PROJECT_ROOT/frontend" -type f -name '*.js' ! -path '*/bindings/*' ! -path '*/vendor/*' -print0)

mkdir -p "$(dirname "$BINARY")"
go build -trimpath -tags production -ldflags "-s -w -X codexrelay/internal/desktop.applicationVersion=$VERSION" -o "$BINARY" ./cmd

install -D -m 0644 "$PROJECT_ROOT/build/linux/codexrelay.desktop" "$PACKAGE_ROOT/usr/share/applications/codexrelay.desktop"
install -D -m 0644 "$PROJECT_ROOT/logo.png" "$PACKAGE_ROOT/usr/share/icons/hicolor/512x512/apps/codexrelay.png"

DEPENDENCIES="$(dpkg-shlibdeps -O -pcodexrelay -e "$BINARY" | sed -n 's/^shlibs:Depends=//p')"
mkdir -p "$PACKAGE_ROOT/DEBIAN"
{
  printf 'Package: codexrelay\n'
  printf 'Version: %s\n' "$VERSION"
  printf 'Section: utils\n'
  printf 'Priority: optional\n'
  printf 'Architecture: %s\n' "$ARCH"
  printf 'Maintainer: CodexRelay <noreply@codexrelay.local>\n'
  if [[ -n "$DEPENDENCIES" ]]; then
    printf 'Depends: %s\n' "$DEPENDENCIES"
  fi
  printf 'Description: Codex API relay and account sync tool\n'
  printf ' CodexRelay is a graphical local API relay and account sync tool.\n'
} > "$PACKAGE_ROOT/DEBIAN/control"

dpkg-deb --build --root-owner-group "$PACKAGE_ROOT" "$DEB"
echo "Packaged: $DEB"
