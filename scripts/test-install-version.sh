#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/conductor-install-version.XXXXXX")
trap 'rm -rf "$TEST_ROOT"' EXIT HUP INT TERM

mkdir -p "$TEST_ROOT/package/scripts" "$TEST_ROOT/package/dist" "$TEST_ROOT/fake-bin" "$TEST_ROOT/home"
cp "$ROOT/scripts/install.sh" "$ROOT/scripts/semver.sh" "$ROOT/scripts/runtime-json.sh" "$TEST_ROOT/package/scripts/"

printf '%s\n' '#!/bin/sh' 'case "$1" in' '  -s) echo Darwin ;;' '  -m) echo arm64 ;;' '  *) echo Darwin ;;' 'esac' > "$TEST_ROOT/fake-bin/uname"
printf '%s\n' '#!/bin/sh' 'test "${1:-}" = version' 'echo conductor 0.3.1' > "$TEST_ROOT/package/dist/conductor-darwin-arm64"
chmod 0755 "$TEST_ROOT/fake-bin/uname" "$TEST_ROOT/package/dist/conductor-darwin-arm64"

actual=$(
  unset VERSION
  PATH="$TEST_ROOT/fake-bin:$PATH" HOME="$TEST_ROOT/home" \
    "$TEST_ROOT/package/scripts/install.sh" --check-version
)
test "$actual" = "0.3.1"

if PATH="$TEST_ROOT/fake-bin:$PATH" HOME="$TEST_ROOT/home" VERSION=0.3.0 "$TEST_ROOT/package/scripts/install.sh" --check-version >/dev/null 2>&1; then
  echo "explicit mismatched VERSION was accepted" >&2
  exit 1
fi
