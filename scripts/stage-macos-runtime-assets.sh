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

# NOTE: The Go proxy binary (cmd/talka-asr-runtime) is already built and
# placed by the Xcode "Build Go Binaries" phase. The C++ FunASR binary
# (funasr-wss-server-2pass) must also be staged if it has been built.
# The Go proxy starts the C++ binary as a subprocess when --funasr-binary
# is provided.
FUNASR_CPP_BINARY="${TALKA_FUNASR_CPP_BINARY:-$ROOT_DIR/build/funasr-runtime/build/bin/funasr-wss-server-2pass}"
if [ -x "$FUNASR_CPP_BINARY" ]; then
	cp -f "$FUNASR_CPP_BINARY" "$RESOURCE_DIR/funasr-wss-server-2pass"
	chmod +x "$RESOURCE_DIR/funasr-wss-server-2pass"
	printf 'STAGED funasr-wss-server-2pass from %s\n' "$FUNASR_CPP_BINARY"
else
	printf 'WARNING: C++ FunASR binary not found at %s (embedded ASR will not work)\n' "$FUNASR_CPP_BINARY" >&2
fi

printf 'APP_ASSETS_READY=%s\n' "$APP_PATH"
