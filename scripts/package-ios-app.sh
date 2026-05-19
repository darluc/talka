#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
CONFIGURATION="Release"
DIST_DIR="${ROOT_DIR}/dist"

show_usage() {
  printf 'usage: %s [--configuration <name>] [--dist-dir <dir>]\n' "$0"
}

usage() {
  show_usage >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
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

PROJECT_PATH="${ROOT_DIR}/apps/ios/TalkaIOS/TalkaIOS.xcodeproj"
SCHEME="TalkaIOS"
SIM_DERIVED_DATA_PATH="${ROOT_DIR}/build/ios-simulator"
DEVICE_DERIVED_DATA_PATH="${ROOT_DIR}/build/ios-device"
SIM_APP_PATH="${SIM_DERIVED_DATA_PATH}/Build/Products/${CONFIGURATION}-iphonesimulator/TalkaIOS.app"
DEVICE_APP_PATH="${DEVICE_DERIVED_DATA_PATH}/Build/Products/${CONFIGURATION}-iphoneos/TalkaIOS.app"
SIM_ZIP_PATH="${DIST_DIR}/TalkaIOS-iOS-simulator.zip"
DEVICE_ZIP_PATH="${DIST_DIR}/TalkaIOS-iOS-device-unsigned.zip"

mkdir -p "$DIST_DIR"
rm -rf "$SIM_DERIVED_DATA_PATH" "$DEVICE_DERIVED_DATA_PATH" "$SIM_ZIP_PATH" "$DEVICE_ZIP_PATH"

xcodebuild \
  -project "$PROJECT_PATH" \
  -scheme "$SCHEME" \
  -configuration "$CONFIGURATION" \
  -sdk iphonesimulator \
  -derivedDataPath "$SIM_DERIVED_DATA_PATH" \
  CODE_SIGNING_ALLOWED=NO \
  build

if [ ! -d "$SIM_APP_PATH" ]; then
  printf 'packaging failed: missing simulator app bundle at %s\n' "$SIM_APP_PATH" >&2
  exit 1
fi

cd "$(dirname "$SIM_APP_PATH")"
ditto -c -k --sequesterRsrc --keepParent "TalkaIOS.app" "$SIM_ZIP_PATH"

xcodebuild \
  -project "$PROJECT_PATH" \
  -scheme "$SCHEME" \
  -configuration "$CONFIGURATION" \
  -sdk iphoneos \
  -destination 'generic/platform=iOS' \
  -derivedDataPath "$DEVICE_DERIVED_DATA_PATH" \
  CODE_SIGNING_ALLOWED=NO \
  build

if [ ! -d "$DEVICE_APP_PATH" ]; then
  printf 'packaging failed: missing device app bundle at %s\n' "$DEVICE_APP_PATH" >&2
  exit 1
fi

cd "$(dirname "$DEVICE_APP_PATH")"
ditto -c -k --sequesterRsrc --keepParent "TalkaIOS.app" "$DEVICE_ZIP_PATH"

printf 'SIMULATOR_APP=%s\n' "$SIM_APP_PATH"
printf 'SIMULATOR_ZIP=%s\n' "$SIM_ZIP_PATH"
printf 'DEVICE_APP=%s\n' "$DEVICE_APP_PATH"
printf 'DEVICE_ZIP=%s\n' "$DEVICE_ZIP_PATH"
