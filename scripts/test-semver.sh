#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/semver.sh"

assert_compare() {
  expected=$1
  left=$2
  right=$3
  actual=$(semver_compare "$left" "$right")
  if [ "$actual" != "$expected" ]; then
    echo "semver_compare $left $right = $actual, expected $expected" >&2
    exit 1
  fi
}

assert_compare -1 0.3.0 0.4.0
assert_compare 0 0.3.0 0.3.0
assert_compare 0 0.3.0+build.2 0.3.0+build.9
assert_compare 1 1.0.0 1.0.0-rc.1
assert_compare -1 1.0.0-alpha.2 1.0.0-alpha.10
assert_compare -1 1.0.0-1 1.0.0-alpha

if semver_compare 1.2 1.2.0 >/dev/null 2>&1; then
  echo "invalid semantic version was accepted" >&2
  exit 1
fi
if semver_compare 1.0.0-invalid@tag 1.0.0 >/dev/null 2>&1; then
  echo "invalid prerelease identifier was accepted" >&2
  exit 1
fi
if semver_compare 1.0.0+ 1.0.0 >/dev/null 2>&1; then
  echo "empty build metadata was accepted" >&2
  exit 1
fi
if conductor_version_from_output "unexpected 0.3.0" >/dev/null 2>&1; then
  echo "unexpected CLI version output was accepted" >&2
  exit 1
fi

test "$(conductor_version_from_output 'conductor 0.3.0')" = "0.3.0"
