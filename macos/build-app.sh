#!/usr/bin/env sh
# Builds "Ratio Connector.app" (menu bar app + the bank-connector program,
# both universal) and a .dmg with a drag-to-Applications layout.
#
#   macos/build-app.sh                                   ad-hoc signed, for local use
#   SIGN_IDENTITY="Developer ID Application: Name (TEAMID)" \
#   NOTARY_PROFILE=ratio-notary macos/build-app.sh       signed and notarized for distribution
#
# NOTARY_PROFILE is a keychain profile created once with
#   xcrun notarytool store-credentials ratio-notary --apple-id ... --team-id ...
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
IDENTITY="${SIGN_IDENTITY:--}"
OUT="$ROOT/dist/macos"
APP="$OUT/Ratio Connector.app"
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"

rm -rf "$OUT"
mkdir -p "$OUT"

echo "==> bank-connector (arm64 + amd64)"
for ARCH in arm64 amd64; do
  (cd "$ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=$ARCH go build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildDate=$DATE" \
    -o "$OUT/bank-connector-$ARCH" ./cmd/connector)
done
lipo -create -output "$OUT/bank-connector" "$OUT/bank-connector-arm64" "$OUT/bank-connector-amd64"

echo "==> menu bar app (arm64 + x86_64)"
swift build -c release --package-path "$ROOT/macos" --arch arm64 --arch x86_64
SWIFT_BIN="$(swift build -c release --package-path "$ROOT/macos" --arch arm64 --arch x86_64 --show-bin-path)/RatioConnector"

echo "==> assembling the app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$SWIFT_BIN" "$APP/Contents/MacOS/RatioConnector"
cp "$OUT/bank-connector" "$APP/Contents/Resources/bank-connector"
cp icons/AppIcon.icns icons/MenuBarIcon*.png "$APP/Contents/Resources/"
cat >"$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>com.ratio-fin.connector</string>
  <key>CFBundleName</key><string>Ratio Connector</string>
  <key>CFBundleDisplayName</key><string>Ratio Connector</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundleExecutable</key><string>RatioConnector</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$(echo "${VERSION#v}" | sed -E "s/^([0-9]+(\.[0-9]+)*).*/\1/")</string>
  <key>CFBundleVersion</key><string>$(date -u +%Y%m%d%H%M)</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHumanReadableCopyright</key><string>Ratio</string>
</dict>
</plist>
PLIST

echo "==> signing ($IDENTITY)"
# Hardened runtime is required for notarization; the inner program is signed first.
codesign --force --timestamp --options runtime --sign "$IDENTITY" "$APP/Contents/Resources/bank-connector"
codesign --force --timestamp --options runtime --sign "$IDENTITY" "$APP"
codesign --verify --deep --strict "$APP"

echo "==> disk image"
STAGE="$OUT/stage"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
DMG="$OUT/RatioConnector-${VERSION#v}.dmg"
hdiutil create -volname "Ratio Connector" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
rm -rf "$STAGE"
[ "$IDENTITY" = "-" ] || codesign --force --timestamp --sign "$IDENTITY" "$DMG"

if [ -n "${NOTARY_PROFILE:-}" ] && [ "$IDENTITY" != "-" ]; then
  echo "==> notarizing"
  xcrun notarytool submit "$DMG" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$DMG"
else
  echo "Not notarized: macOS will warn on first open. Set SIGN_IDENTITY and NOTARY_PROFILE for a release."
fi

( cd "$OUT" && shasum -a 256 "$(basename "$DMG")" >"$(basename "$DMG").sha256" )
echo "Done: $DMG"
