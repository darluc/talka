#!/bin/sh
set -eu

SIGNING_TARGET="${1:-}"

usage() {
  printf 'usage: %s macos|ios\n' "$0" >&2
  exit 2
}

require_env() {
  name="$1"
  eval "value=\${$name:-}"
  if [ -z "$value" ]; then
    printf 'signing setup failed: missing required environment variable %s\n' "$name" >&2
    exit 1
  fi
}

decode_secret() {
  value="$1"
  output_path="$2"
  printf '%s' "$value" | base64 -D > "$output_path"
}

case "$SIGNING_TARGET" in
  macos|ios)
    ;;
  *)
    usage
    ;;
esac

require_env BUILD_KEYCHAIN_PASSWORD

KEYCHAIN_PATH="${RUNNER_TEMP:-/tmp}/talka-signing.keychain-db"
security create-keychain -p "$BUILD_KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$BUILD_KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security list-keychains -d user -s "$KEYCHAIN_PATH" $(security list-keychains -d user | sed 's/[ "]//g')

case "$SIGNING_TARGET" in
  macos)
    require_env MACOS_DEVELOPER_ID_CERT_BASE64
    require_env MACOS_DEVELOPER_ID_CERT_PASSWORD
    CERT_PATH="${RUNNER_TEMP:-/tmp}/macos-developer-id.p12"
    decode_secret "$MACOS_DEVELOPER_ID_CERT_BASE64" "$CERT_PATH"
    security import "$CERT_PATH" -k "$KEYCHAIN_PATH" -P "$MACOS_DEVELOPER_ID_CERT_PASSWORD" -T /usr/bin/codesign -T /usr/bin/security
    ;;
  ios)
    require_env IOS_DISTRIBUTION_CERT_BASE64
    require_env IOS_DISTRIBUTION_CERT_PASSWORD
    require_env IOS_PROVISIONING_PROFILE_BASE64
    CERT_PATH="${RUNNER_TEMP:-/tmp}/ios-distribution.p12"
    PROFILE_PATH="${RUNNER_TEMP:-/tmp}/talka-ios.mobileprovision"
    PROFILE_PLIST="${RUNNER_TEMP:-/tmp}/talka-ios-profile.plist"
    PROFILE_DIR="$HOME/Library/MobileDevice/Provisioning Profiles"
    decode_secret "$IOS_DISTRIBUTION_CERT_BASE64" "$CERT_PATH"
    decode_secret "$IOS_PROVISIONING_PROFILE_BASE64" "$PROFILE_PATH"
    security import "$CERT_PATH" -k "$KEYCHAIN_PATH" -P "$IOS_DISTRIBUTION_CERT_PASSWORD" -T /usr/bin/codesign -T /usr/bin/security
    mkdir -p "$PROFILE_DIR"
    security cms -D -i "$PROFILE_PATH" > "$PROFILE_PLIST"
    PROFILE_UUID="$(/usr/libexec/PlistBuddy -c 'Print :UUID' "$PROFILE_PLIST")"
    cp "$PROFILE_PATH" "$PROFILE_DIR/$PROFILE_UUID.mobileprovision"
    printf 'IOS_PROVISIONING_PROFILE_UUID=%s\n' "$PROFILE_UUID"
    ;;
esac

security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$BUILD_KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security find-identity -v -p codesigning "$KEYCHAIN_PATH"
