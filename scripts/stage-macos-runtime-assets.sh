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
MODEL_SOURCE_DIR="${TALKA_MODEL_SOURCE_DIR:-$ROOT_DIR/models/funasr}"
MODEL_DEST_DIR="$RESOURCE_DIR/models/funasr"

for required in \
  "$MODEL_SOURCE_DIR/paraformer-zh-onnx/model_quant.onnx" \
  "$MODEL_SOURCE_DIR/paraformer-zh-online-onnx/model_quant.onnx" \
  "$MODEL_SOURCE_DIR/fsmn-vad-onnx/model_quant.onnx" \
  "$MODEL_SOURCE_DIR/ct-punc-onnx/model_quant.onnx" \
  "$MODEL_SOURCE_DIR/itn-zh/zh_itn_tagger.fst" \
  "$MODEL_SOURCE_DIR/itn-zh/zh_itn_verbalizer.fst"
do
  if [ ! -f "$required" ]; then
    printf 'error: missing embedded FunASR asset %s\n' "$required" >&2
    exit 1
  fi
done

mkdir -p "$MODEL_DEST_DIR" "$FRAMEWORKS_DIR"
rsync -a --delete "$MODEL_SOURCE_DIR/" "$MODEL_DEST_DIR/"
: > "$RESOURCE_DIR/hotwords.txt"

"$ROOT_DIR/scripts/build-funasr-runtime.sh" \
  --runtime-dir "$RESOURCE_DIR" \
  --frameworks-dir "$FRAMEWORKS_DIR"

printf 'APP_ASSETS_READY=%s\n' "$APP_PATH"
