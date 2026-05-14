#!/bin/sh
set -eu

usage() {
  printf 'usage: %s --fixtures <wav-dir-or-file> [--model-dir <dir>] [--output <tsv>] [--threads <n>]\n' "$0" >&2
  exit 2
}

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
fixtures=""
model_dir="$ROOT_DIR/models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en"
output=""
threads=2

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fixtures)
      [ -n "${2-}" ] || usage
      fixtures="$2"
      shift 2
      ;;
    --model-dir)
      [ -n "${2-}" ] || usage
      model_dir="$2"
      shift 2
      ;;
    --output)
      [ -n "${2-}" ] || usage
      output="$2"
      shift 2
      ;;
    --threads)
      [ -n "${2-}" ] || usage
      threads="$2"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

[ -n "$fixtures" ] || usage
[ -e "$fixtures" ] || {
  printf 'error: fixtures path not found: %s\n' "$fixtures" >&2
  exit 1
}

if [ -z "$output" ]; then
  output="$ROOT_DIR/build/sherpa-onnx-benchmark/$(date +%Y%m%d-%H%M%S).tsv"
fi
mkdir -p "$(dirname "$output")"

wav_list="$(mktemp)"
transcriber="$(mktemp -t talka-sherpa-transcribe.XXXXXX)"
cleanup() {
  rm -f "$wav_list" "$transcriber"
}
trap cleanup EXIT INT TERM

if [ -d "$fixtures" ]; then
  find "$fixtures" -type f -name '*.wav' | sort > "$wav_list"
else
  printf '%s\n' "$fixtures" > "$wav_list"
fi

if [ ! -s "$wav_list" ]; then
  printf 'error: no .wav fixtures found in %s\n' "$fixtures" >&2
  exit 1
fi

printf 'fixture\tprecision\telapsed_ms\ttranscript\n' > "$output"

cgo_ldflags_allow="${CGO_LDFLAGS_ALLOW:--Wl,-rpath,.*}"
cgo_ldflags="${CGO_LDFLAGS:-} -Wl,-rpath,$ROOT_DIR/third_party/sherpa-onnx/lib"
CGO_LDFLAGS_ALLOW="$cgo_ldflags_allow" \
CGO_LDFLAGS="$cgo_ldflags" \
GOCACHE="${GOCACHE:-/private/tmp/talka-go-cache}" \
go build -tags sherpa_onnx -o "$transcriber" ./cmd/talka-sherpa-transcribe

while IFS= read -r fixture; do
  for precision in int8 fp32; do
    line="$(
      "$transcriber" \
        --fixture "$fixture" \
        --model-dir "$model_dir" \
        --precision "$precision" \
        --threads "$threads"
    )"
    elapsed_ms="$(printf '%s\n' "$line" | awk -F '\t' '{print $3}')"
    transcript="$(printf '%s\n' "$line" | awk -F '\t' '{print $4}')"
    printf '%s\t%s\t%s\t%s\n' "$fixture" "$precision" "$elapsed_ms" "$transcript" >> "$output"
  done
done < "$wav_list"

printf 'SHERPA_ONNX_BENCHMARK_READY=%s\n' "$output"
