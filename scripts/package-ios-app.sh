#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
CONFIGURATION="Release"
DIST_DIR="${ROOT_DIR}/dist"
DEVICE_SIGNING="${IOS_DEVICE_SIGNING:-unsigned}"
EXPORT_METHOD="${IOS_EXPORT_METHOD:-ad-hoc}"

show_usage() {
  printf 'usage: %s [--configuration <name>] [--dist-dir <dir>] [--device-signing unsigned|signed]\n' "$0"
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
    --device-signing)
      [ -n "${2-}" ] || usage
      DEVICE_SIGNING="$2"
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
ARCHIVE_PATH="${DEVICE_DERIVED_DATA_PATH}/TalkaIOS.xcarchive"
EXPORT_PATH="${DIST_DIR}/ios-export"
EXPORT_OPTIONS_PATH="${DEVICE_DERIVED_DATA_PATH}/ExportOptions.plist"
SIM_ZIP_PATH="${DIST_DIR}/TalkaIOS-iOS-simulator.zip"
DEVICE_ZIP_PATH="${DIST_DIR}/TalkaIOS-iOS-device-unsigned.zip"
DEVICE_IPA_PATH="${DIST_DIR}/TalkaIOS-iOS-device-signed.ipa"

mkdir -p "$DIST_DIR"
rm -rf "$SIM_DERIVED_DATA_PATH" "$DEVICE_DERIVED_DATA_PATH" "$SIM_ZIP_PATH" "$DEVICE_ZIP_PATH" "$DEVICE_IPA_PATH" "$EXPORT_PATH"

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

case "$DEVICE_SIGNING" in
  unsigned)
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
    printf 'DEVICE_APP=%s\n' "$DEVICE_APP_PATH"
    printf 'DEVICE_ZIP=%s\n' "$DEVICE_ZIP_PATH"
    ;;
  signed)
    [ -n "${APPLE_TEAM_ID:-}" ] || {
      printf 'packaging failed: signed iOS export requires APPLE_TEAM_ID\n' >&2
      exit 1
    }
    [ -n "${IOS_PROVISIONING_PROFILE_NAME:-}" ] || {
      printf 'packaging failed: signed iOS export requires IOS_PROVISIONING_PROFILE_NAME\n' >&2
      exit 1
    }

    mkdir -p "$DEVICE_DERIVED_DATA_PATH"
    cat > "$EXPORT_OPTIONS_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key>
  <string>${EXPORT_METHOD}</string>
  <key>signingStyle</key>
  <string>manual</string>
  <key>teamID</key>
  <string>${APPLE_TEAM_ID}</string>
  <key>provisioningProfiles</key>
  <dict>
    <key>talkaios.zvz.im</key>
    <string>${IOS_PROVISIONING_PROFILE_NAME}</string>
  </dict>
  <key>stripSwiftSymbols</key>
  <true/>
</dict>
</plist>
EOF

    xcodebuild \
      -project "$PROJECT_PATH" \
      -scheme "$SCHEME" \
      -configuration "$CONFIGURATION" \
      -sdk iphoneos \
      -destination 'generic/platform=iOS' \
      -archivePath "$ARCHIVE_PATH" \
      DEVELOPMENT_TEAM="$APPLE_TEAM_ID" \
      CODE_SIGN_IDENTITY="${IOS_CODE_SIGN_IDENTITY:-Apple Distribution}" \
      CODE_SIGN_STYLE=Manual \
      PROVISIONING_PROFILE_SPECIFIER="$IOS_PROVISIONING_PROFILE_NAME" \
      CODE_SIGNING_ALLOWED=YES \
      CODE_SIGNING_REQUIRED=YES \
      archive

    xcodebuild \
      -exportArchive \
      -archivePath "$ARCHIVE_PATH" \
      -exportPath "$EXPORT_PATH" \
      -exportOptionsPlist "$EXPORT_OPTIONS_PATH"

    IPA_PATH="$(find "$EXPORT_PATH" -maxdepth 1 -name '*.ipa' -print -quit)"
    [ -n "$IPA_PATH" ] || {
      printf 'packaging failed: signed export did not produce an ipa in %s\n' "$EXPORT_PATH" >&2
      exit 1
    }
    mv "$IPA_PATH" "$DEVICE_IPA_PATH"
    printf 'DEVICE_ARCHIVE=%s\n' "$ARCHIVE_PATH"
    printf 'DEVICE_IPA=%s\n' "$DEVICE_IPA_PATH"
    ;;
  *)
    printf 'packaging failed: unsupported device signing mode %s\n' "$DEVICE_SIGNING" >&2
    exit 2
    ;;
esac

printf 'SIMULATOR_APP=%s\n' "$SIM_APP_PATH"
printf 'SIMULATOR_ZIP=%s\n' "$SIM_ZIP_PATH"
