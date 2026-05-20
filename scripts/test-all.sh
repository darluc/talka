#!/bin/sh
set -eu

if [ -z "${DEVELOPER_DIR-}" ] && [ -d /Applications/Xcode.app/Contents/Developer ]; then
  export DEVELOPER_DIR="/Applications/Xcode.app/Contents/Developer"
fi

ios_simulator_name="${IOS_SIMULATOR_NAME:-iPhone 16}"
ios_simulator_id="$(xcrun simctl list devices available | grep -E "^[[:space:]]*${ios_simulator_name} \\(" | head -n 1 | sed -E 's/.*\(([A-F0-9-]+)\).*/\1/')"
if [ -z "$ios_simulator_id" ]; then
  runtime_id="$(xcrun simctl list runtimes available | grep '^iOS ' | tail -n 1 | sed 's/.* - //')"
  devicetype_id="$(xcrun simctl list devicetypes | grep "^${ios_simulator_name} (" | sed -E 's/.*\((com\.apple\.CoreSimulator\.SimDeviceType\.[^)]+)\).*/\1/' | head -n 1)"

  if [ -z "$devicetype_id" ] || [ -z "$runtime_id" ]; then
    printf 'unable to provision %s simulator from available Xcode device types/runtimes\n' "$ios_simulator_name" >&2
    exit 1
  fi

  ios_simulator_id="$(xcrun simctl create "$ios_simulator_name" "$devicetype_id" "$runtime_id")"
fi

./scripts/setup-dev.sh --verify-only
./scripts/prepare-test-fixtures.sh

GO_TEST_TAGS="${GO_TEST_TAGS:-}"
if [ -d "./third_party/sherpa-onnx/include" ] && [ -d "./third_party/sherpa-onnx/lib" ]; then
  case " $GO_TEST_TAGS " in
    *" sherpa_onnx "*) ;;
    *) GO_TEST_TAGS="${GO_TEST_TAGS:+$GO_TEST_TAGS }sherpa_onnx" ;;
  esac
  export CGO_LDFLAGS_ALLOW="${CGO_LDFLAGS_ALLOW:-(-Wl,-rpath,.*|-L.*|-l.*)}"
  export CGO_LDFLAGS="${CGO_LDFLAGS:-} -Wl,-rpath,$(pwd)/third_party/sherpa-onnx/lib"
  export DYLD_LIBRARY_PATH="$(pwd)/third_party/sherpa-onnx/lib${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"
fi

GO_TEST_PACKAGES="$(go list ./... | grep -v '^talka/build/')"

if [ -n "$GO_TEST_TAGS" ]; then
  go test -tags "$GO_TEST_TAGS" $GO_TEST_PACKAGES
else
  go test $GO_TEST_PACKAGES
fi
xcodebuild test -workspace apps/Talka.xcworkspace -scheme TalkaMac -destination 'platform=macOS'
xcrun simctl boot "$ios_simulator_id" >/dev/null 2>&1 || true
xcrun simctl bootstatus "$ios_simulator_id" -b
xcrun simctl terminate "$ios_simulator_id" talkaios.zvz.im >/dev/null 2>&1 || true
xcodebuild test -workspace apps/Talka.xcworkspace -scheme TalkaIOS -destination "platform=iOS Simulator,id=$ios_simulator_id" -only-testing:TalkaIOSTests
