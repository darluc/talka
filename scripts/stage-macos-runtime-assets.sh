#!/bin/sh
set -eu

usage() {
  printf 'usage: %s --app-path <TalkaMac.app>\n' "$0" >&2
  exit 2
}

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
APP_PATH=""

while [ $# -gt 0 ]; do
  case "$1" in
    --app-path)
      [ -n "${2-}" ] || usage
      APP_PATH="$2"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

[ -n "$APP_PATH" ] || usage
[ -d "$APP_PATH" ] || {
  printf 'error: app bundle not found at %s\n' "$APP_PATH" >&2
  exit 1
}

RESOURCE_DIR="$APP_PATH/Contents/Resources"
FRAMEWORKS_DIR="$APP_PATH/Contents/Frameworks"
SHERPA_MODEL_SOURCE_DIR="${TALKA_SHERPA_MODEL_SOURCE_DIR:-$ROOT_DIR/models/sherpa-onnx}"
SHERPA_MODEL_DEST_DIR="$RESOURCE_DIR/models/sherpa-onnx"
SHERPA_LIB_SOURCE_DIR="${TALKA_SHERPA_LIB_SOURCE_DIR:-$ROOT_DIR/third_party/sherpa-onnx/lib}"

mkdir -p "$FRAMEWORKS_DIR"

SHERPA_DEFAULT_MODEL_DIR="$SHERPA_MODEL_SOURCE_DIR/streaming-paraformer-trilingual-zh-cantonese-en"
SHERPA_BILINGUAL_MODEL_DIR="$SHERPA_MODEL_SOURCE_DIR/streaming-paraformer-bilingual-zh-en"
for required in \
  "$SHERPA_DEFAULT_MODEL_DIR/tokens.txt" \
  "$SHERPA_DEFAULT_MODEL_DIR/encoder.int8.onnx" \
  "$SHERPA_DEFAULT_MODEL_DIR/decoder.int8.onnx" \
  "$SHERPA_BILINGUAL_MODEL_DIR/tokens.txt" \
  "$SHERPA_BILINGUAL_MODEL_DIR/encoder.int8.onnx" \
  "$SHERPA_BILINGUAL_MODEL_DIR/decoder.int8.onnx"
do
  if [ ! -f "$required" ]; then
    printf 'error: missing embedded sherpa-onnx asset %s\n' "$required" >&2
    exit 1
  fi
done
mkdir -p "$SHERPA_MODEL_DEST_DIR"
rsync -a --delete "$SHERPA_MODEL_SOURCE_DIR/" "$SHERPA_MODEL_DEST_DIR/"

if ! ls "$SHERPA_LIB_SOURCE_DIR"/libsherpa-onnx-c-api*.dylib >/dev/null 2>&1; then
  printf 'error: missing sherpa-onnx C API dylib in %s; run scripts/build-sherpa-onnx-runtime.sh\n' "$SHERPA_LIB_SOURCE_DIR" >&2
  exit 1
fi
cp "$SHERPA_LIB_SOURCE_DIR"/*.dylib "$FRAMEWORKS_DIR/"

printf 'APP_ASSETS_READY=%s\n' "$APP_PATH"
