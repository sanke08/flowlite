#!/usr/bin/env bash
# Build the macOS release into app/v<version>/.
#
#   ./build-macos.sh                 ad-hoc signature (no Apple account needed)
#   SIGN_ID="Developer ID Application: You (TEAMID)" ./build-macos.sh
set -euo pipefail
cd "$(dirname "$0")"

VERSION="$(sed -n 's/^__version__ = "\(.*\)"/\1/p' flowlite/__init__.py)"
ARCH="$(uname -m)"
OUT="app/v${VERSION}"
DMG="${OUT}/FlowLite-${VERSION}-macOS-${ARCH}.dmg"
APP="dist/FlowLite.app"

if [ ! -d .venv ]; then
  echo "No .venv — run ./run.sh once first to set up." >&2
  exit 1
fi

echo "==> FlowLite ${VERSION} (macOS ${ARCH})"
rm -rf build dist
.venv/bin/pyinstaller FlowLite.spec --noconfirm

echo "==> Signing"
if [ -n "${SIGN_ID:-}" ]; then
  # Nested code must be signed before the bundle that contains it.
  find "$APP/Contents/Frameworks" -type f \( -name "*.dylib" -o -name "*.so" \) \
    -exec codesign --force --timestamp --sign "$SIGN_ID" {} + 2>/dev/null || true
  codesign --force --timestamp --options runtime \
    --identifier com.flowlite.app --sign "$SIGN_ID" "$APP"
else
  codesign --force --identifier com.flowlite.app --sign - "$APP"
  echo "    ad-hoc — recipients must clear the quarantine flag once (see app/README.md)"
fi
codesign --verify --strict "$APP" && echo "    signature OK"

echo "==> Packaging DMG"
mkdir -p "$OUT"
rm -f "$DMG"
STAGE="$(mktemp -d)"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
# Ship the un-quarantine helper next to the app so it is impossible to miss.
cp app/Fix-Gatekeeper.command "$STAGE/" 2>/dev/null || true
hdiutil create -volname "FlowLite ${VERSION}" -srcfolder "$STAGE" \
  -ov -format UDZO -quiet "$DMG"
rm -rf "$STAGE"

shasum -a 256 "$DMG" | awk '{print $1"  "substr($2, match($2, /[^\/]*$/))}' \

echo
echo "Release artifact:"
echo "  ${DMG}  ($(du -h "$DMG" | cut -f1))"
