#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${VERSION:-0.2.1}
BIN_DIR=${CONDUCTOR_BIN_DIR:-"$HOME/.local/bin"}
DEST="$BIN_DIR/conductor"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "Conductor's bundled release binaries currently target macOS." >&2
  echo "Build from source with 'make build' for development on another Unix platform." >&2
  exit 1
fi

case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64) ARCH=amd64 ;;
  *)
    echo "Unsupported macOS architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

SOURCE="$ROOT/dist/conductor-darwin-$ARCH"
BUILD_FROM_SOURCE=${CONDUCTOR_BUILD_FROM_SOURCE:-0}
if [ "$BUILD_FROM_SOURCE" = "1" ] || [ ! -x "$SOURCE" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "Building Conductor from source..."
    mkdir -p "$ROOT/dist"
    (
      cd "$ROOT"
      CGO_ENABLED=0 GOOS=darwin GOARCH="$ARCH" \
        go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
        -o "$SOURCE" ./cmd/conductor
    )
  else
    echo "Missing $SOURCE and Go is not installed." >&2
    echo "A source checkout does not contain prebuilt binaries." >&2
    echo "Download and extract a GitHub Release archive, or install Go 1.22+ and retry." >&2
    exit 1
  fi
fi

mkdir -p "$BIN_DIR"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "$SOURCE" "$DEST"
else
  cp "$SOURCE" "$DEST"
  chmod 0755 "$DEST"
fi

if ! "$DEST" version >/dev/null 2>&1; then
  if command -v go >/dev/null 2>&1; then
    echo "Bundled binary could not run; rebuilding a local native binary..."
    (
      cd "$ROOT"
      go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
        -o "$DEST" ./cmd/conductor
    )
    chmod 0755 "$DEST"
  else
    echo "The installed binary could not run. It may be quarantined or corrupt." >&2
    echo "Install Go and retry with CONDUCTOR_BUILD_FROM_SOURCE=1, or review macOS security settings." >&2
    exit 1
  fi
fi

"$DEST" init
"$DEST" hooks install

echo
echo "Installed Conductor: $DEST"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo "Add this directory to PATH: $BIN_DIR"
    echo "For zsh:  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc"
    ;;
esac

echo
echo "Next steps:"
echo "  codex features enable goals   # only if /goal is missing"
echo "  conductor doctor"
echo "  conductor project init <name> # for namespaced multi-project sessions"
echo "  Restart Codex sessions, open /hooks, inspect and trust the Conductor hooks."
