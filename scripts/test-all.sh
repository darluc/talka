#!/bin/sh
set -eu

if [ -z "${DEVELOPER_DIR-}" ] && [ -d /Applications/Xcode.app/Contents/Developer ]; then
  export DEVELOPER_DIR="/Applications/Xcode.app/Contents/Developer"
fi

if ! xcrun simctl list devices available | grep -F "iPhone 16 (" >/dev/null 2>&1; then
  runtime_id="$(xcrun simctl list runtimes available | grep '^iOS ' | tail -n 1 | sed 's/.* - //')"
  devicetype_id="$(xcrun simctl list devicetypes | grep '^iPhone 16 (' | sed -E 's/.*\((com\.apple\.CoreSimulator\.SimDeviceType\.[^)]+)\).*/\1/' | head -n 1)"

  if [ -z "$devicetype_id" ] || [ -z "$runtime_id" ]; then
    printf 'unable to provision iPhone 16 simulator alias from available Xcode device types/runtimes\n' >&2
    exit 1
  fi

  xcrun simctl create "iPhone 16" "$devicetype_id" "$runtime_id" >/dev/null
fi

./scripts/setup-dev.sh --verify-only
go test ./...
xcodebuild test -workspace apps/Talka.xcworkspace -scheme TalkaMac -destination 'platform=macOS'
xcodebuild test -workspace apps/Talka.xcworkspace -scheme TalkaIOS -destination 'platform=iOS Simulator,name=iPhone 16'
