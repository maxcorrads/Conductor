#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/semver.sh"
. "$ROOT/scripts/runtime-json.sh"
VERSION=${VERSION:-}
BIN_DIR=${CONDUCTOR_BIN_DIR:-"$HOME/.local/bin"}
DEST="$BIN_DIR/conductor"
CONDUCTOR_DATA_HOME=${CONDUCTOR_HOME:-"$HOME/.conductor"}

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
    if [ -z "$VERSION" ]; then
      if command -v make >/dev/null 2>&1; then
        VERSION=$(make -s -C "$ROOT" print-version)
      else
        echo "VERSION is required when building from source without make." >&2
        exit 1
      fi
    fi
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

source_output=$("$SOURCE" version) || {
  echo "The release binary could not report its version. No installed CLI was changed." >&2
  exit 1
}
source_version=$(conductor_version_from_output "$source_output") || {
  echo "The release binary reported an invalid version: $source_output" >&2
  exit 1
}
if [ -n "$VERSION" ] && [ "$source_version" != "$VERSION" ]; then
  echo "Release version mismatch: expected $VERSION, binary reports $source_version. No installed CLI was changed." >&2
  exit 1
fi
if [ "${1:-}" = "--check-version" ]; then
  printf '%s\n' "$source_version"
  exit 0
fi
if [ "$#" -gt 0 ]; then
  echo "usage: ./scripts/install.sh [--check-version]" >&2
  exit 1
fi

replace_cli=1
kept_installed_cli=0
installed_newer_cli=0
if [ -x "$DEST" ]; then
  if installed_output=$("$DEST" version 2>/dev/null); then
    installed_version=$(conductor_version_from_output "$installed_output") || {
      echo "The installed CLI reported an invalid version: $installed_output" >&2
      echo "It was left unchanged." >&2
      exit 1
    }
    comparison=$(semver_compare "$installed_version" "$source_version") || {
      echo "Could not compare installed version $installed_version with release $source_version." >&2
      echo "The installed CLI was left unchanged." >&2
      exit 1
    }
    case "$comparison" in
      0)
        replace_cli=0
        kept_installed_cli=1
        ;;
      1)
        replace_cli=0
        kept_installed_cli=1
        installed_newer_cli=1
        ;;
    esac
  fi
fi

if [ "$installed_newer_cli" != "1" ]; then
  for candidate in "$CONDUCTOR_DATA_HOME/config.json" "$CONDUCTOR_DATA_HOME/state.json" "$CONDUCTOR_DATA_HOME"/projects/*/state.json; do
    [ -f "$candidate" ] || continue
    runtime_version=$(runtime_root_version "$candidate") || {
      echo "Could not read the top-level runtime version from $candidate." >&2
      echo "No binary was replaced." >&2
      exit 1
    }
    case "$runtime_version" in
      2) ;;
      1)
        echo "Conductor $source_version cannot use version 1 runtime data: $candidate" >&2
        echo "No binary was replaced. Move $CONDUCTOR_DATA_HOME aside, then rerun the installer." >&2
        exit 1
        ;;
      *)
        echo "Conductor $source_version cannot use runtime schema $runtime_version in $candidate." >&2
        echo "No binary was replaced." >&2
        exit 1
        ;;
    esac
  done
fi

if [ "$replace_cli" = "1" ]; then
  mkdir -p "$BIN_DIR"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$SOURCE" "$DEST"
  else
    cp "$SOURCE" "$DEST"
    chmod 0755 "$DEST"
  fi
elif [ "$kept_installed_cli" = "1" ]; then
  echo "Keeping installed Conductor $installed_version (release: $source_version)."
fi

if ! "$DEST" version >/dev/null 2>&1; then
  if command -v go >/dev/null 2>&1; then
    echo "Bundled binary could not run; rebuilding a local native binary..."
    (
      cd "$ROOT"
      go build -trimpath -ldflags "-s -w -X main.version=$source_version" \
        -o "$DEST" ./cmd/conductor
    )
    chmod 0755 "$DEST"
    rebuilt_output=$("$DEST" version) || {
      echo "The rebuilt CLI could not report its version." >&2
      exit 1
    }
    rebuilt_version=$(conductor_version_from_output "$rebuilt_output") || {
      echo "The rebuilt CLI reported an invalid version: $rebuilt_output" >&2
      exit 1
    }
    if [ "$rebuilt_version" != "$source_version" ]; then
      echo "Rebuilt CLI version mismatch: expected $source_version, got $rebuilt_version." >&2
      exit 1
    fi
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
