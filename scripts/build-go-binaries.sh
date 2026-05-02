#!/bin/sh
set -eu

OUTPUT_DIR="./bin"
FOR_BUNDLE=""

while [ $# -gt 0 ]; do
  case "${1}" in
    --output-dir)
      if [ -z "${2-}" ]; then
        printf 'error: --output-dir requires a directory argument\n' >&2
        exit 2
      fi
      OUTPUT_DIR="${2}"
      shift 2
      ;;
    --for-bundle)
      FOR_BUNDLE="yes"
      shift
      ;;
    --help|-h)
      printf 'usage: %s [--output-dir <dir>] [--for-bundle]\n' "$0"
      printf '\nBuilds talka-server and talka-asr-runtime Go binaries for macOS.\n'
      printf 'Set ARCHS to "arm64 x86_64" to build for both architectures.\n'
      printf 'Use --for-bundle to output clean names (talka-server, talka-asr-runtime)\n'
      printf 'for embedding inside an app bundle.\n'
      exit 0
      ;;
    *)
      printf 'usage: %s [--output-dir <dir>] [--for-bundle]\n' "$0" >&2
      exit 2
      ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  printf 'error: go is not installed\n' >&2
  exit 1
fi

printf 'Building Go binaries for macOS\n'
printf 'Go version: %s\n' "$(go version)"
printf 'Output dir: %s\n' "$OUTPUT_DIR"

ARCHS="${ARCHS:-arm64}"

for arch in $ARCHS; do
  case "$arch" in
    arm64)
      goarch="arm64"
      ;;
    x86_64)
      goarch="amd64"
      ;;
    *)
      printf 'error: unsupported architecture: %s\n' "$arch" >&2
      exit 1
      ;;
  esac

  export GOOS="darwin"
  export GOARCH="$goarch"

  for binary in talka-server talka-asr-runtime; do
    if [ -n "$FOR_BUNDLE" ]; then
      output="${OUTPUT_DIR}/${binary}"
    else
      output="${OUTPUT_DIR}/${binary}_darwin_${goarch}"
    fi
    printf '  building %s (%s)...\n' "$binary" "darwin/${goarch}"
    go build -trimpath -ldflags="-s -w" -o "$output" "./cmd/${binary}/"
    chmod +x "$output"
    printf '    -> %s\n' "$output"
  done
done

printf 'Done.\n'
