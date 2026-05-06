#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
DERIVED_DATA_PATH="${ROOT_DIR}/build/macos"
PRODUCTS_DIR="${DERIVED_DATA_PATH}/Build/Products/Release"
APP_PATH="${PRODUCTS_DIR}/TalkaMac.app"
DIST_DIR="${ROOT_DIR}/dist"
ZIP_PATH="${DIST_DIR}/TalkaMac-macOS.zip"

mkdir -p "$DIST_DIR"
rm -rf "$DERIVED_DATA_PATH" "$ZIP_PATH"

cd "$ROOT_DIR/apps"
xcodebuild \
  -workspace Talka.xcworkspace \
  -scheme TalkaMac \
  -configuration Release \
  -derivedDataPath "$DERIVED_DATA_PATH" \
  build

if [ ! -d "$APP_PATH" ]; then
  printf 'packaging failed: missing app bundle at %s\n' "$APP_PATH" >&2
  exit 1
fi

"$ROOT_DIR/scripts/stage-macos-runtime-assets.sh" --app-path "$APP_PATH"

codesign --force --deep --sign - "$APP_PATH"
codesign --verify --deep --strict "$APP_PATH"

cd "$PRODUCTS_DIR"
ditto -c -k --sequesterRsrc --keepParent "TalkaMac.app" "$ZIP_PATH"

printf 'APP=%s\n' "$APP_PATH"
printf 'ZIP=%s\n' "$ZIP_PATH"
