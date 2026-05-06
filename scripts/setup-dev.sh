#!/bin/sh
set -eu

MODE="verify"
if [ "${1-}" = "--verify-only" ]; then
  MODE="verify"
elif [ "${1-}" = "" ]; then
  MODE="verify"
else
  printf 'usage: %s [--verify-only]\n' "$0" >&2
  exit 2
fi

missing=0
xcode_dev_dir="${DEVELOPER_DIR-}"

if [ -z "$xcode_dev_dir" ] && [ -d /Applications/Xcode.app/Contents/Developer ]; then
  xcode_dev_dir="/Applications/Xcode.app/Contents/Developer"
fi

if [ -n "$xcode_dev_dir" ]; then
  export DEVELOPER_DIR="$xcode_dev_dir"
fi

printf 'Talka setup verification (%s mode)\n' "$MODE"
printf 'Platform: %s %s\n' "$(uname -s)" "$(uname -m)"
if [ -n "${DEVELOPER_DIR-}" ]; then
  printf 'Developer dir: %s\n' "$DEVELOPER_DIR"
fi

for tool in go xcodebuild xcrun swift python3 shasum; do
  if command -v "$tool" >/dev/null 2>&1; then
    printf '[ok] required tool: %s (%s)\n' "$tool" "$(command -v "$tool")"
  else
    printf '[missing] required tool: %s\n' "$tool" >&2
    missing=1
  fi
done

if ! xcodebuild -version >/dev/null 2>&1; then
  printf '[missing] full Xcode developer directory is not active or unavailable\n' >&2
  missing=1
fi

if command -v ollama >/dev/null 2>&1; then
  printf '[optional] ollama available: %s\n' "$(command -v ollama)"
else
  printf '[optional] ollama unavailable (expected for scaffold-only work)\n'
fi

if [ -f models/funasr/checksums.txt ]; then
  printf '[ok] model manifest: models/funasr/checksums.txt\n'
else
  printf '[missing] model manifest: models/funasr/checksums.txt\n' >&2
  missing=1
fi

for dir in models/funasr/paraformer-zh-onnx models/funasr/paraformer-zh-online-onnx models/funasr/fsmn-vad-onnx models/funasr/ct-punc-onnx models/funasr/itn-zh .sisyphus/evidence; do
  if [ -d "$dir" ]; then
    printf '[ok] expected directory: %s\n' "$dir"
  else
    printf '[missing] expected directory: %s\n' "$dir" >&2
    missing=1
  fi
done

if [ "$missing" -ne 0 ]; then
  printf 'Setup verification failed: install missing prerequisites or create missing scaffold assets.\n' >&2
  exit 1
fi

printf 'Setup verification passed.\n'
