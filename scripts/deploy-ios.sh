#!/bin/bash
# deploy-ios.sh — Build TalkaIOS and install it onto a connected physical iPhone.
#
# Usage:
#   ./scripts/deploy-ios.sh                     # auto-detect first available device
#   ./scripts/deploy-ios.sh --device ZVVZ       # match by device name
#   ./scripts/deploy-ios.sh --launch            # also launch the app after install
#   ./scripts/deploy-ios.sh --clean             # clean build first
#
# Prerequisites:
#   - Xcode.app installed (not just Command Line Tools)
#   - An Apple Developer account signed in via Xcode > Settings > Accounts
#   - A paired iPhone connected via USB with Developer Mode enabled
#   - The DEVELOPMENT_TEAM / signing identity must be valid for dev.talka.ios

set -euo pipefail

# ── Defaults ──────────────────────────────────────────────────────────────────
WORKSPACE="apps/Talka.xcworkspace"
SCHEME="TalkaIOS"
BUNDLE_ID="dev.talka.ios"
DERIVED_DATA_PATH="/tmp/talka-ios-device-build"
DEVELOPMENT_TEAM="${TALKA_IOS_TEAM:-475398SZ3P}"
CODE_SIGN_IDENTITY="Apple Development"
DEVICE_FILTER=""
LAUNCH_AFTER_INSTALL=false
CLEAN_BUILD=false

# ── Resolve repo root ────────────────────────────────────────────────────────
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKSPACE_PATH="$REPO_ROOT/$WORKSPACE"

# ── Ensure Xcode.app toolchain ───────────────────────────────────────────────
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
if ! command -v xcodebuild &>/dev/null; then
  echo "ERROR: xcodebuild not found. Install Xcode.app and ensure DEVELOPER_DIR is set." >&2
  exit 1
fi

# ── Parse arguments ──────────────────────────────────────────────────────────
usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Options:
  --device NAME    Target device name substring (e.g. ZVVZ). Default: first available.
  --launch         Launch the app on device after installing.
  --clean          Clean build directory before building.
  --team TEAM_ID   Development team ID (default: 475398SZ3P or TALKA_IOS_TEAM env).
  -h, --help       Show this help.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --device)
      shift
      DEVICE_FILTER="${1:-}"
      ;;
    --launch)
      LAUNCH_AFTER_INSTALL=true
      ;;
    --clean)
      CLEAN_BUILD=true
      ;;
    --team)
      shift
      DEVELOPMENT_TEAM="${1:-}"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

# ── Discover connected device ────────────────────────────────────────────────
echo "▸ Discovering connected devices..."
DEVICE_TABLE=$(xcrun devicectl list devices 2>/dev/null)

# Find the CoreDevice ID for the target device
if [[ -n "$DEVICE_FILTER" ]]; then
  DEVICE_LINE=$(echo "$DEVICE_TABLE" | grep -i "$DEVICE_FILTER" | grep -i "available" | head -1)
else
  DEVICE_LINE=$(echo "$DEVICE_TABLE" | grep -i "available" | head -1)
fi

if [[ -z "$DEVICE_LINE" ]]; then
  echo "ERROR: No available device found." >&2
  echo "Available devices:" >&2
  echo "$DEVICE_TABLE" >&2
  exit 1
fi

COREDEVICE_ID=$(echo "$DEVICE_LINE" | awk '{print $3}')
DEVICE_NAME=$(echo "$DEVICE_LINE" | awk '{print $1}')

echo "  Target device : $DEVICE_NAME"
echo "  CoreDevice ID : $COREDEVICE_ID"

# Get hardware UDID for xcodebuild -destination.
# devicectl install/launch uses CoreDevice ID, but xcodebuild needs hardware UDID.
# The most reliable way is to ask xcodebuild itself via -showDestinations.
HARDWARE_UDID=$(xcodebuild \
  -workspace "$WORKSPACE_PATH" \
  -scheme "$SCHEME" \
  -showDestinations 2>/dev/null \
  | grep -A5 "platform :iOS" \
  | grep "name" \
  | head -1 || true)

# Parse the UDID from showDestinations output
# Format: { platform:iOS, arch:arm64, id:00008110-0014190C2132801E, name:ZVVZ }
HARDWARE_UDID=$(xcodebuild \
  -workspace "$WORKSPACE_PATH" \
  -scheme "$SCHEME" \
  -showDestinations 2>/dev/null \
  | grep -E "platform.*iOS.*id:" \
  | grep -v "Simulator" \
  | grep -v "placeholder" \
  | sed 's/.*id:\([A-Fa-f0-9-]*\).*/\1/' \
  | head -1 || true)

if [[ -z "$HARDWARE_UDID" ]]; then
  echo "WARNING: Could not determine hardware UDID automatically." >&2
  echo "  Will try building with generic/platform=iOS." >&2
  DEST_ARG="generic/platform=iOS"
else
  echo "  Hardware UDID: $HARDWARE_UDID"
  DEST_ARG="id=$HARDWARE_UDID"
fi

# ── Clean (optional) ─────────────────────────────────────────────────────────
if [[ "$CLEAN_BUILD" == true ]]; then
  echo "▸ Cleaning build directory..."
  rm -rf "$DERIVED_DATA_PATH"
fi

# ── Build ─────────────────────────────────────────────────────────────────────
echo "▸ Building TalkaIOS for device (team=$DEVELOPMENT_TEAM)..."
xcodebuild \
  -workspace "$WORKSPACE_PATH" \
  -scheme "$SCHEME" \
  -destination "$DEST_ARG" \
  -configuration Debug \
  -derivedDataPath "$DERIVED_DATA_PATH" \
  DEVELOPMENT_TEAM="$DEVELOPMENT_TEAM" \
  CODE_SIGN_IDENTITY="$CODE_SIGN_IDENTITY" \
  CODE_SIGN_STYLE=Automatic \
  CODE_SIGNING_ALLOWED=YES \
  CODE_SIGNING_REQUIRED=YES \
  -allowProvisioningUpdates \
  -allowProvisioningDeviceRegistration \
  build

APP_PATH="$DERIVED_DATA_PATH/Build/Products/Debug-iphoneos/TalkaIOS.app"

if [[ ! -d "$APP_PATH" ]]; then
  echo "ERROR: Build succeeded but .app not found at $APP_PATH" >&2
  exit 1
fi

echo "  Build output : $APP_PATH"

# ── Install ───────────────────────────────────────────────────────────────────
echo "▸ Installing on $DEVICE_NAME..."
xcrun devicectl device install app --device "$COREDEVICE_ID" "$APP_PATH"

echo "▸ Installed $BUNDLE_ID on $DEVICE_NAME ✓"

# ── Launch (optional) ────────────────────────────────────────────────────────
if [[ "$LAUNCH_AFTER_INSTALL" == true ]]; then
  echo "▸ Launching $BUNDLE_ID..."
  xcrun devicectl device process launch --device "$COREDEVICE_ID" "$BUNDLE_ID"
  echo "▸ Launched $BUNDLE_ID ✓"
fi

echo ""
echo "Done. TalkaIOS is on your phone."
if [[ "$LAUNCH_AFTER_INSTALL" == false ]]; then
  echo "Tip: pass --launch to also start the app, or run:"
  echo "  xcrun devicectl device process launch --device $COREDEVICE_ID $BUNDLE_ID"
fi
