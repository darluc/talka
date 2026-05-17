#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
MODEL_PROFILE="${SHERPA_ONNX_MODEL_PROFILE:-trilingual}"
MODEL_REVISION="${SHERPA_ONNX_MODEL_REVISION:-main}"
case "$MODEL_PROFILE" in
  trilingual)
    DEFAULT_MODEL_NAME="sherpa-onnx-streaming-paraformer-trilingual-zh-cantonese-en"
    DEFAULT_MODEL_REPO="csukuangfj/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
    DEFAULT_MODEL_DIR="$ROOT_DIR/models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en"
    DEFAULT_BASE_URL=""
    ;;
  bilingual)
    DEFAULT_MODEL_NAME="sherpa-onnx-streaming-paraformer-bilingual-zh-en"
    DEFAULT_MODEL_REPO="csukuangfj/sherpa-onnx-streaming-paraformer-bilingual-zh-en"
    DEFAULT_MODEL_DIR="$ROOT_DIR/models/sherpa-onnx/streaming-paraformer-bilingual-zh-en"
    DEFAULT_BASE_URL="https://huggingface.co/$DEFAULT_MODEL_REPO/resolve/$MODEL_REVISION"
    ;;
  *)
    printf 'error: unsupported SHERPA_ONNX_MODEL_PROFILE %s; use trilingual or bilingual\n' "$MODEL_PROFILE" >&2
    exit 2
    ;;
esac
MODEL_NAME="${SHERPA_ONNX_MODEL_NAME:-$DEFAULT_MODEL_NAME}"
MODEL_REPO="${SHERPA_ONNX_MODEL_REPO:-$DEFAULT_MODEL_REPO}"
MODEL_DIR="${SHERPA_ONNX_MODEL_DIR:-$DEFAULT_MODEL_DIR}"
ARCHIVE_NAME="${SHERPA_ONNX_MODEL_ARCHIVE_NAME:-${MODEL_NAME}.tar.bz2}"
ARCHIVE_URL="${SHERPA_ONNX_MODEL_ARCHIVE_URL:-https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/${ARCHIVE_NAME}}"
ARCHIVE_PATH="${SHERPA_ONNX_MODEL_ARCHIVE_PATH:-$ROOT_DIR/build/sherpa-onnx-models/$ARCHIVE_NAME}"
BASE_URL="${SHERPA_ONNX_MODEL_BASE_URL:-$DEFAULT_BASE_URL}"

mkdir -p "$MODEL_DIR" "$(dirname "$ARCHIVE_PATH")"

download_url() {
  url="$1"
  output="$2"
  printf 'Downloading %s\n' "$url"
  curl -fL --retry 3 --connect-timeout "${SHERPA_ONNX_CONNECT_TIMEOUT:-60}" -o "$output" "$url"
}

download_file() {
  name="$1"
  output="$MODEL_DIR/$name"
  if [ -f "$output" ]; then
	return 0
  fi
  if download_url "$BASE_URL/$name" "$output"; then
    return 0
  fi
  rm -f "$output"

  for base in ${SHERPA_ONNX_MODEL_MIRRORS:-https://hf-mirror.com/${MODEL_REPO}/resolve/${MODEL_REVISION}}; do
    case "$base" in
      */) url="${base}${name}" ;;
      *) url="${base}/${name}" ;;
    esac
    if download_url "$url" "$output"; then
      return 0
    fi
    rm -f "$output"
  done

  printf 'error: failed to download %s. Set SHERPA_ONNX_MODEL_BASE_URL or SHERPA_ONNX_MODEL_MIRRORS and retry.\n' "$name" >&2
  exit 1
}

download_archive() {
  if [ -f "$ARCHIVE_PATH" ]; then
    return 0
  fi
  if download_url "$ARCHIVE_URL" "$ARCHIVE_PATH"; then
    return 0
  fi
  rm -f "$ARCHIVE_PATH"

  for base in ${SHERPA_ONNX_MODEL_MIRRORS:-}; do
    case "$base" in
      */) url="${base}${ARCHIVE_NAME}" ;;
      *) url="${base}/${ARCHIVE_NAME}" ;;
    esac
    if download_url "$url" "$ARCHIVE_PATH"; then
      return 0
    fi
    rm -f "$ARCHIVE_PATH"
  done

  printf 'error: failed to download %s. Set SHERPA_ONNX_MODEL_ARCHIVE_URL or SHERPA_ONNX_MODEL_MIRRORS and retry.\n' "$ARCHIVE_NAME" >&2
  exit 1
}

if [ -n "$BASE_URL" ]; then
  download_file tokens.txt
  download_file encoder.int8.onnx
  download_file decoder.int8.onnx
else
  download_archive
  rm -rf "$MODEL_DIR.tmp"
  mkdir -p "$MODEL_DIR.tmp"
  tar -xjf "$ARCHIVE_PATH" -C "$MODEL_DIR.tmp"
  EXTRACTED_DIR="$(find "$MODEL_DIR.tmp" -type d -name "$MODEL_NAME" | head -1)"
  if [ -z "$EXTRACTED_DIR" ]; then
    printf 'error: %s not found inside %s\n' "$MODEL_NAME" "$ARCHIVE_PATH" >&2
    exit 1
  fi
  rsync -a --delete "$EXTRACTED_DIR/" "$MODEL_DIR/"
  rm -rf "$MODEL_DIR.tmp"
fi

for required in tokens.txt encoder.int8.onnx decoder.int8.onnx; do
  if [ ! -f "$MODEL_DIR/$required" ]; then
    printf 'error: missing downloaded sherpa-onnx model file %s\n' "$MODEL_DIR/$required" >&2
    exit 1
  fi
done

printf 'SHERPA_ONNX_MODEL_READY=%s\n' "$MODEL_DIR"
