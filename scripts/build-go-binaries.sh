#!/bin/sh
set -eu

OUTPUT_DIR="./bin"
FOR_BUNDLE=""
BINARIES="${BINARIES:-talka-server talka-asr-runtime}"

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
    --binaries)
      if [ -z "${2-}" ]; then
        printf 'error: --binaries requires a space-separated binary list\n' >&2
        exit 2
      fi
      BINARIES="${2}"
      shift 2
      ;;
    --help|-h)
      printf 'usage: %s [--output-dir <dir>] [--for-bundle] [--binaries \"name1 name2\"]\n' "$0"
      printf '\nBuilds Go binaries for macOS.\n'
      printf 'Set ARCHS to "arm64 x86_64" to build for both architectures.\n'
      printf 'Use --for-bundle to output clean names (for example talka-server)\n'
      printf 'for embedding inside an app bundle.\n'
      exit 0
      ;;
    *)
      printf 'usage: %s [--output-dir <dir>] [--for-bundle] [--binaries \"name1 name2\"]\n' "$0" >&2
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
printf 'Binaries: %s\n' "$BINARIES"

ARCHS="${ARCHS:-arm64}"
GO_TAGS="${GO_TAGS:-}"
if [ -d "./third_party/sherpa-onnx/include" ] && [ -d "./third_party/sherpa-onnx/lib" ]; then
  case " $GO_TAGS " in
    *" sherpa_onnx "*) ;;
    *) GO_TAGS="${GO_TAGS:+$GO_TAGS }sherpa_onnx" ;;
  esac
fi
if [ -n "$GO_TAGS" ]; then
  printf 'Go build tags: %s\n' "$GO_TAGS"
  export CGO_LDFLAGS_ALLOW="${CGO_LDFLAGS_ALLOW:-(-Wl,-rpath,.*|-L.*|-l.*)}"
  if printf '%s\n' "$GO_TAGS" | grep -q 'sherpa_onnx'; then
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} -Wl,-rpath,@executable_path/../Frameworks -Wl,-rpath,$(pwd)/third_party/sherpa-onnx/lib"
  fi
fi

mkdir -p "$OUTPUT_DIR"

temp_dir=""
cleanup() {
  if [ -n "$temp_dir" ] && [ -d "$temp_dir" ]; then
    rm -rf "$temp_dir"
  fi
}
trap cleanup EXIT INT TERM

if [ -n "$FOR_BUNDLE" ]; then
  temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/talka-go-build.XXXXXX")"
fi

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
  export CGO_ENABLED=1
  export CC="clang -arch ${arch}"

  for binary in $BINARIES; do
    if [ -n "$FOR_BUNDLE" ]; then
      output="${temp_dir}/${binary}_${goarch}"
    else
      output="${OUTPUT_DIR}/${binary}_darwin_${goarch}"
    fi
    printf '  building %s (%s)...\n' "$binary" "darwin/${goarch}"
    if [ -n "$GO_TAGS" ]; then
      go build -tags "$GO_TAGS" -trimpath -ldflags="-s -w" -o "$output" "./cmd/${binary}/"
    else
      go build -trimpath -ldflags="-s -w" -o "$output" "./cmd/${binary}/"
    fi
    chmod +x "$output"
    printf '    -> %s\n' "$output"
  done
done

if [ -n "$FOR_BUNDLE" ]; then
  arch_count=0
  for _arch in $ARCHS; do
    arch_count=$((arch_count + 1))
  done

  for binary in $BINARIES; do
    final_output="${OUTPUT_DIR}/${binary}"
    if [ "$arch_count" -eq 1 ]; then
      mv "${temp_dir}/${binary}_"* "$final_output"
      chmod +x "$final_output"
      printf '  bundled %s -> %s\n' "$binary" "$final_output"
      continue
    fi

    inputs=""
    for arch in $ARCHS; do
      case "$arch" in
        arm64) goarch="arm64" ;;
        x86_64) goarch="amd64" ;;
      esac
      inputs="$inputs ${temp_dir}/${binary}_${goarch}"
    done

    # shellcheck disable=SC2086
    lipo -create $inputs -output "$final_output"
    chmod +x "$final_output"
    printf '  bundled %s (universal) -> %s\n' "$binary" "$final_output"
  done
fi

printf 'Done.\n'
