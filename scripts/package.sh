#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${VERSION:-0.2.0}
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
  prompts \
  scripts; do
  cp -R "$ROOT/$item" "$STAGE/"
done

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
