#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
VERSION="${SHERPA_ONNX_VERSION:-v1.13.2}"
VERSION_NO_V="${VERSION#v}"
WORK_DIR="${SHERPA_ONNX_WORK_DIR:-$ROOT_DIR/build/sherpa-onnx}"
THIRD_PARTY_DIR="${SHERPA_ONNX_THIRD_PARTY_DIR:-$ROOT_DIR/third_party/sherpa-onnx}"
RELEASE_BASE="https://github.com/k2-fsa/sherpa-onnx/releases/download/${VERSION}"
ARCHIVE_NAME="${SHERPA_ONNX_ARCHIVE_NAME:-sherpa-onnx-${VERSION}-osx-universal2-shared-no-tts-lib.tar.bz2}"
ARCHIVE_URL="${SHERPA_ONNX_ARCHIVE_URL:-$RELEASE_BASE/$ARCHIVE_NAME}"
ARCHIVE_PATH="$WORK_DIR/$ARCHIVE_NAME"
HEADER_URL="${SHERPA_ONNX_HEADER_URL:-https://raw.githubusercontent.com/k2-fsa/sherpa-onnx/${VERSION}/sherpa-onnx/c-api/c-api.h}"

mkdir -p "$WORK_DIR" "$THIRD_PARTY_DIR"

download_url() {
  url="$1"
  output="$2"
  if [ -f "$output" ]; then
	return 0
  fi
  printf 'Downloading %s\n' "$url"
  curl -fL --retry 3 --connect-timeout "${SHERPA_ONNX_CONNECT_TIMEOUT:-60}" -o "$output" "$url"
}

download_archive() {
  if download_url "$ARCHIVE_URL" "$ARCHIVE_PATH"; then
    return 0
  fi
  rm -f "$ARCHIVE_PATH"

  for base in ${SHERPA_ONNX_RELEASE_MIRRORS:-}; do
    case "$base" in
      */) url="${base}${ARCHIVE_NAME}" ;;
      *) url="${base}/${ARCHIVE_NAME}" ;;
    esac
    if download_url "$url" "$ARCHIVE_PATH"; then
      return 0
    fi
    rm -f "$ARCHIVE_PATH"
  done

  printf 'error: failed to download %s. Set SHERPA_ONNX_ARCHIVE_URL or SHERPA_ONNX_RELEASE_MIRRORS and retry.\n' "$ARCHIVE_NAME" >&2
  exit 1
}

download_archive

rm -rf "$WORK_DIR/prebuilt"
mkdir -p "$WORK_DIR/prebuilt"
tar -xjf "$ARCHIVE_PATH" -C "$WORK_DIR/prebuilt"

rm -rf "$THIRD_PARTY_DIR/include" "$THIRD_PARTY_DIR/lib"
mkdir -p "$THIRD_PARTY_DIR/include/sherpa-onnx/c-api" "$THIRD_PARTY_DIR/lib"

HEADER="$(find "$WORK_DIR/prebuilt" -path '*/sherpa-onnx/c-api/c-api.h' -type f | head -1)"
if [ -z "$HEADER" ] && [ -f "$WORK_DIR/source/sherpa-onnx/c-api/c-api.h" ]; then
  HEADER="$WORK_DIR/source/sherpa-onnx/c-api/c-api.h"
fi
if [ -z "$HEADER" ]; then
  HEADER_DOWNLOAD="$WORK_DIR/c-api.h"
  if download_url "$HEADER_URL" "$HEADER_DOWNLOAD"; then
    HEADER="$HEADER_DOWNLOAD"
  else
    rm -f "$HEADER_DOWNLOAD"
  fi
fi
if [ -z "$HEADER" ]; then
  printf 'error: c-api.h not found. Set SHERPA_ONNX_HEADER_URL, clone https://github.com/k2-fsa/sherpa-onnx into %s/source, or use a prebuilt archive with headers.\n' "$WORK_DIR" >&2
  exit 1
fi
cp "$HEADER" "$THIRD_PARTY_DIR/include/sherpa-onnx/c-api/c-api.h"

find "$WORK_DIR/prebuilt" -name '*.dylib' -type f -exec cp {} "$THIRD_PARTY_DIR/lib/" \;
if ! ls "$THIRD_PARTY_DIR/lib"/libsherpa-onnx-c-api*.dylib >/dev/null 2>&1; then
  printf 'error: libsherpa-onnx-c-api*.dylib not found in %s\n' "$ARCHIVE_PATH" >&2
  exit 1
fi

printf 'SHERPA_ONNX_READY=%s\n' "$THIRD_PARTY_DIR"
