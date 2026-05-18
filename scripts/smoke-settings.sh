#!/bin/sh
set -eu

ollama_url="http://localhost:11434"
model="qwen3:8b"
asr_model="models/sherpa-onnx/streaming-paraformer-bilingual-zh-en/encoder.int8.onnx"

usage() {
  printf 'usage: %s [--ollama-url URL] [--model MODEL] [--asr-model PATH]\n' "$0" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --ollama-url)
      shift
      ollama_url="${1:-}"
      ;;
    --model)
      shift
      model="${1:-}"
      ;;
    --asr-model)
      shift
      asr_model="${1:-}"
      ;;
    *)
      usage
      exit 2
      ;;
  esac
  shift
done

output="$(go run ./internal/app/smokesettings --ollama-url "$ollama_url" --model "$model" --asr-model "$asr_model")"
printf '%s\n' "$output"

printf '%s\n' "$output" | grep -q '^CONFIG_OK '
printf '%s\n' "$output" | grep -q '^VALIDATION_ERROR field=asr.sherpa_onnx.decoder_path$'
printf '%s\n' "$output" | grep -q '^SECRET_SCAN_OK$'
