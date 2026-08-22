#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${VERSION:-0.4.2}
BUILD_NUMBER=${BUILD_NUMBER:-1}
GO_COMMAND=${GO_COMMAND:-go}
APP_ROOT="$ROOT/macos/ConductorApp"
BUILD_ROOT=${APP_BUILD_ROOT:-"$ROOT/build/macos-app"}
APP="$ROOT/release/Conductor.app"
ARCHIVE="$ROOT/release/Conductor-v$VERSION-macos.zip"
CHECKSUM="$ARCHIVE.sha256"
ICON_SOURCE="$ROOT/docs/images/conductor-icon.png"
ICONSET="$BUILD_ROOT/Conductor.iconset"
SWIFT_BUILD="$BUILD_ROOT/swift"
CLI_ARM64="$BUILD_ROOT/conductor-darwin-arm64"
CLI_AMD64="$BUILD_ROOT/conductor-darwin-amd64"
CLI_UNIVERSAL="$BUILD_ROOT/conductor"

if ! printf '%s\n' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "VERSION must be three dot-separated integers for CFBundleShortVersionString, got: $VERSION" >&2
  exit 1
fi

case "$BUILD_ROOT" in
  "$ROOT"/build/*|/private/tmp/conductor-*|/tmp/conductor-*) ;;
  *)
    echo "APP_BUILD_ROOT must be inside $ROOT/build or a conductor-* directory under /tmp" >&2
    exit 1
    ;;
esac
case "/$BUILD_ROOT/" in
  */../*|*/./*)
    echo "APP_BUILD_ROOT must not contain traversal components" >&2
    exit 1
    ;;
esac

case "$BUILD_NUMBER" in
  ''|*[!0-9]*)
    echo "BUILD_NUMBER must be a positive integer, got: $BUILD_NUMBER" >&2
    exit 1
    ;;
esac
if [ "$BUILD_NUMBER" -eq 0 ]; then
  echo "BUILD_NUMBER must be greater than zero" >&2
  exit 1
fi

rm -rf "$BUILD_ROOT" "$APP" "$ARCHIVE" "$CHECKSUM"
mkdir -p "$BUILD_ROOT" "$APP/Contents/MacOS" "$APP/Contents/Resources" "$ROOT/release"

CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 "$GO_COMMAND" build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" -o "$CLI_ARM64" ./cmd/conductor
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 "$GO_COMMAND" build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" -o "$CLI_AMD64" ./cmd/conductor
lipo -create "$CLI_ARM64" "$CLI_AMD64" -output "$CLI_UNIVERSAL"

swift build \
  --package-path "$APP_ROOT" \
  --scratch-path "$SWIFT_BUILD" \
  --configuration release \
  --arch arm64 \
  --arch x86_64 \
  --product Conductor

cp "$SWIFT_BUILD/apple/Products/Release/Conductor" "$APP/Contents/MacOS/Conductor"
cp "$CLI_UNIVERSAL" "$APP/Contents/Resources/conductor"
cp "$APP_ROOT/Resources/Info.plist" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD_NUMBER" "$APP/Contents/Info.plist"

mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
  double=$((size * 2))
  sips -z "$size" "$size" "$ICON_SOURCE" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  sips -z "$double" "$double" "$ICON_SOURCE" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/Conductor.icns"
chmod 0755 "$APP/Contents/MacOS/Conductor" "$APP/Contents/Resources/conductor"

if [ -n "${SIGNING_IDENTITY:-}" ]; then
  codesign --force --timestamp --options runtime --sign "$SIGNING_IDENTITY" "$APP/Contents/Resources/conductor"
  codesign --force --timestamp --options runtime \
    --entitlements "$APP_ROOT/Resources/Conductor.entitlements" \
    --sign "$SIGNING_IDENTITY" "$APP"
else
  codesign --force --sign - "$APP/Contents/Resources/conductor"
  codesign --force --deep --sign - "$APP"
fi

codesign --verify --deep --strict --verbose=2 "$APP"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$ARCHIVE"
(
  cd "$ROOT/release"
  shasum -a 256 "$(basename "$ARCHIVE")" > "$(basename "$CHECKSUM")"
)

echo "$APP"
echo "$ARCHIVE"
