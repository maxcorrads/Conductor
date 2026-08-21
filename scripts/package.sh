#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${VERSION:-0.4.0}
if ! printf '%s\n' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$'; then
  echo "VERSION must be a semantic version, got: $VERSION" >&2
  exit 1
fi
NAME="conductor-v$VERSION"
STAGE="$ROOT/release/$NAME"
ARCHIVE="$ROOT/release/$NAME.zip"

rm -rf "$STAGE" "$ARCHIVE"
mkdir -p "$STAGE"

for item in \
  .editorconfig \
  .github \
  .gitignore \
  CHANGELOG.md \
  CONTRIBUTING.md \
  LICENSE \
  Makefile \
  README.md \
  cmd \
  config.example.json \
  docs \
  examples \
  go.mod \
  internal \
  macos \
  prompts \
  scripts; do
  cp -R "$ROOT/$item" "$STAGE/"
done

# Never ship local SwiftPM caches when packaging from a developer checkout.
rm -rf "$STAGE/macos/ConductorApp/.build" "$STAGE/macos/ConductorApp/.swiftpm"

mkdir -p "$STAGE/dist"
cp "$ROOT/dist/conductor-darwin-arm64" "$STAGE/dist/"
cp "$ROOT/dist/conductor-darwin-amd64" "$STAGE/dist/"
cp "$ROOT/dist/CHECKSUMS.txt" "$STAGE/dist/"

find "$STAGE" -name '.DS_Store' -delete
find "$STAGE" -type f -name '*.sh' -exec chmod 0755 {} +

(
  cd "$ROOT/release"
  zip -q -r "$NAME.zip" "$NAME"
)

(
  cd "$ROOT/release"
  shasum -a 256 "$NAME.zip" > "$NAME.zip.sha256"
)
echo "$ARCHIVE"
