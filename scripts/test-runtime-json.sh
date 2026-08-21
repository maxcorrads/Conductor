#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/runtime-json.sh"

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/conductor-runtime-json.XXXXXX")
trap 'rm -rf "$TEST_ROOT"' EXIT HUP INT TERM

printf '%s\n' '{"version":2,"tasks":{"example":{"sent_goal_objective":"nested {\"version\": 1}"}}}' > "$TEST_ROOT/state.json"
test "$(runtime_root_version "$TEST_ROOT/state.json")" = "2"

printf '%s\n' '{"version":1,"nested":{"version":2}}' > "$TEST_ROOT/legacy.json"
test "$(runtime_root_version "$TEST_ROOT/legacy.json")" = "1"

printf '%s\n' 'not-json' > "$TEST_ROOT/invalid.json"
if runtime_root_version "$TEST_ROOT/invalid.json" >/dev/null 2>&1; then
  echo "invalid runtime JSON was accepted" >&2
  exit 1
fi
