#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
CONFIGURATION="Release"
ARCH="universal"
DIST_DIR="${ROOT_DIR}/dist"

show_usage() {
  printf 'usage: %s [--arch arm64|x86_64|universal] [--configuration <name>] [--dist-dir <dir>]\n' "$0"
}

usage() {
  show_usage >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --arch)
      [ -n "${2-}" ] || usage
      ARCH="$2"
      shift 2
      ;;
    --configuration)
      [ -n "${2-}" ] || usage
      CONFIGURATION="$2"
      shift 2
      ;;
    --dist-dir)
      [ -n "${2-}" ] || usage
      DIST_DIR="$2"
      shift 2
      ;;
    --help|-h)
      show_usage
      exit 0
      ;;
    *)
      usage
      ;;
  esac
done

case "$ARCH" in
  arm64)
    BUILD_ARCHS="arm64"
    ARTIFACT_ARCH="arm64"
    ;;
  x86_64)
    BUILD_ARCHS="x86_64"
    ARTIFACT_ARCH="x86_64"
    ;;
  universal)
    BUILD_ARCHS="arm64 x86_64"
    ARTIFACT_ARCH="universal"
    ;;
  *)
    printf 'packaging failed: unsupported arch %s\n' "$ARCH" >&2
    exit 2
    ;;
esac

DERIVED_DATA_PATH="${ROOT_DIR}/build/macos-${ARTIFACT_ARCH}"
PRODUCTS_DIR="${DERIVED_DATA_PATH}/Build/Products/${CONFIGURATION}"
APP_PATH="${PRODUCTS_DIR}/TalkaMac.app"
ZIP_PATH="${DIST_DIR}/TalkaMac-macOS-${ARTIFACT_ARCH}.zip"
BIN_APP_PATH="${ROOT_DIR}/bin/TalkaMac-${ARTIFACT_ARCH}.app"
SIGN_IDENTITY="${MACOS_CODE_SIGN_IDENTITY:--}"
NOTARIZE="${MACOS_NOTARIZE:-0}"
NOTARY_KEY_PATH="${APP_STORE_CONNECT_API_KEY_PATH:-}"

mkdir -p "$DIST_DIR"
rm -rf "$DERIVED_DATA_PATH" "$ZIP_PATH"

cd "$ROOT_DIR/apps"
ARCHS="$BUILD_ARCHS" xcodebuild \
  -workspace Talka.xcworkspace \
  -scheme TalkaMac \
  -configuration "$CONFIGURATION" \
  -derivedDataPath "$DERIVED_DATA_PATH" \
  ARCHS="$BUILD_ARCHS" \
  ONLY_ACTIVE_ARCH=NO \
  build

if [ ! -d "$APP_PATH" ]; then
  printf 'packaging failed: missing app bundle at %s\n' "$APP_PATH" >&2
  exit 1
fi

"$ROOT_DIR/scripts/stage-macos-runtime-assets.sh" --app-path "$APP_PATH"

if [ ! -f "$APP_PATH/Contents/Resources/AppIcon.icns" ]; then
  printf 'packaging failed: missing app icon at %s\n' "$APP_PATH/Contents/Resources/AppIcon.icns" >&2
  exit 1
fi

if ! /usr/libexec/PlistBuddy -c 'Print :CFBundleIconFile' "$APP_PATH/Contents/Info.plist" >/dev/null 2>&1; then
  printf 'packaging failed: Info.plist is missing CFBundleIconFile\n' >&2
  exit 1
fi

if [ "$SIGN_IDENTITY" = "-" ]; then
  codesign --force --deep --sign - "$APP_PATH"
else
  codesign --force --deep --options runtime --timestamp --sign "$SIGN_IDENTITY" "$APP_PATH"
fi
codesign --verify --deep --strict "$APP_PATH"

mkdir -p "${ROOT_DIR}/bin"
rm -rf "$BIN_APP_PATH"
ditto "$APP_PATH" "$BIN_APP_PATH"

cd "$PRODUCTS_DIR"

if [ "$NOTARIZE" = "1" ]; then
  [ "$SIGN_IDENTITY" != "-" ] || {
    printf 'packaging failed: MACOS_NOTARIZE=1 requires MACOS_CODE_SIGN_IDENTITY\n' >&2
    exit 1
  }
  [ -n "$NOTARY_KEY_PATH" ] || {
    printf 'packaging failed: MACOS_NOTARIZE=1 requires APP_STORE_CONNECT_API_KEY_PATH\n' >&2
    exit 1
  }
  [ -n "${APP_STORE_CONNECT_KEY_ID:-}" ] || {
    printf 'packaging failed: MACOS_NOTARIZE=1 requires APP_STORE_CONNECT_KEY_ID\n' >&2
    exit 1
  }
  [ -n "${APP_STORE_CONNECT_ISSUER_ID:-}" ] || {
    printf 'packaging failed: MACOS_NOTARIZE=1 requires APP_STORE_CONNECT_ISSUER_ID\n' >&2
    exit 1
  }

  NOTARY_ZIP="${DIST_DIR}/TalkaMac-macOS-${ARTIFACT_ARCH}-notary.zip"
  rm -f "$NOTARY_ZIP"
  ditto -c -k --sequesterRsrc --keepParent "TalkaMac.app" "$NOTARY_ZIP"
  xcrun notarytool submit "$NOTARY_ZIP" \
    --key "$NOTARY_KEY_PATH" \
    --key-id "$APP_STORE_CONNECT_KEY_ID" \
    --issuer "$APP_STORE_CONNECT_ISSUER_ID" \
    --wait
  xcrun stapler staple "$APP_PATH"
  xcrun stapler validate "$APP_PATH"
  rm -f "$NOTARY_ZIP"
fi

ditto -c -k --sequesterRsrc --keepParent "TalkaMac.app" "$ZIP_PATH"

printf 'APP=%s\n' "$APP_PATH"
printf 'BIN_APP=%s\n' "$BIN_APP_PATH"
printf 'ZIP=%s\n' "$ZIP_PATH"
