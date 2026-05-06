#!/bin/sh
set -eu

fixture="fixtures/audio/zh-short.wav"
default_port=$((22000 + ($$ % 10000)))
addr="${TALKA_REAL_ASR_ADDR:-127.0.0.1:${default_port}}"
url="ws://${addr}/ws"
upstream_url="${TALKA_FUNASR_UPSTREAM_URL:-ws://127.0.0.1:10095}"
model_asr="${TALKA_FUNASR_MODEL_ASR:-models/funasr/paraformer-zh-onnx}"
model_online="${TALKA_FUNASR_MODEL_ONLINE:-models/funasr/paraformer-zh-online-onnx}"
model_vad="${TALKA_FUNASR_MODEL_VAD:-models/funasr/fsmn-vad-onnx}"
model_punc="${TALKA_FUNASR_MODEL_PUNC:-models/funasr/ct-punc-onnx}"
model_itn="${TALKA_FUNASR_MODEL_ITN:-models/funasr/itn-zh}"
ollama_base_url="${TALKA_OLLAMA_BASE_URL:-http://localhost:11434}"
ollama_model="${TALKA_OLLAMA_MODEL:-qwen3:8b}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fixture)
      if [ "$#" -lt 2 ]; then
        printf 'usage: %s [--fixture path]\n' "$0" >&2
        exit 2
      fi
      fixture="$2"
      shift 2
      ;;
    *)
      printf 'usage: %s [--fixture path]\n' "$0" >&2
      exit 2
      ;;
  esac
done

if [ ! -f "$fixture" ]; then
  printf 'missing fixture: %s\n' "$fixture" >&2
  exit 1
fi

for path in "$model_asr" "$model_online" "$model_vad" "$model_punc" "$model_itn"; do
  if [ ! -d "$path" ]; then
    printf 'missing FunASR model path: %s. Download or point TALKA_FUNASR_MODEL_* at local ONNX model directories before running this host integration smoke.\n' "$path" >&2
    exit 1
  fi
  if [ -z "$(find "$path" -type f ! -name '.gitkeep' -print -quit)" ]; then
    printf 'FunASR model path has no real model assets: %s. Replace scaffold placeholders with local ONNX runtime files or point TALKA_FUNASR_MODEL_* at populated model directories.\n' "$path" >&2
    exit 1
  fi
done

if ! curl -fsS "$ollama_base_url/api/tags" >/dev/null 2>&1; then
  printf 'Ollama is not reachable at %s. Start it locally and ensure model %s is available.\n' "$ollama_base_url" "$ollama_model" >&2
  exit 1
fi

log_file="$(mktemp)"
cleanup() {
  if [ "${server_pid-}" != "" ]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -f "$log_file"
}
trap cleanup EXIT INT TERM

go run ./cmd/talka-asr-runtime serve --addr "$addr" --upstream-url "$upstream_url" >"$log_file" 2>&1 &
server_pid=$!

attempt=0
while [ "$attempt" -lt 50 ]; do
  if go run ./cmd/talka-asr-runtime health --url "$url" >/dev/null 2>&1; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.1
done

if ! go run ./cmd/talka-asr-runtime health --url "$url" >/dev/null 2>&1; then
  printf 'real ASR sidecar did not become healthy. Ensure a FunASR websocket runtime is available at %s and reachable from localhost.\n' "$upstream_url" >&2
  cat "$log_file" >&2
  exit 1
fi

output="$(go run ./internal/app/smokefakee2e \
  --fixture "$fixture" \
  --fixture-format wav \
  --full-session \
  --host-integration \
  --asr-url "$url" \
  --asr-timeout 35s \
  --llm-provider ollama \
  --ollama-base-url "$ollama_base_url" \
  --ollama-model "$ollama_model" \
  --ollama-timeout 30s)"
printf '%s\n' "$output"

printf '%s\n' "$output" | grep -q '^HOST_FULL_SESSION kind=host-integration physical_ios=false asr=external llm=ollama injection=fake$'
printf '%s\n' "$output" | grep -q '^ENCRYPTED_AUDIO_START sample_rate=16000 channels=1 encoding=pcm_s16le frame_ms=20$'
printf '%s\n' "$output" | grep -q '^ENCRYPTED_AUDIO_FRAME count=[1-9][0-9]*$'
printf '%s\n' "$output" | grep -q '^ENCRYPTED_AUDIO_STOP last_sequence=[1-9][0-9]*$'
printf '%s\n' "$output" | grep -q '^RAW_ASR .\{1,\}$'
printf '%s\n' "$output" | grep -q '^CLEANUP_STATUS cleaned$'
printf '%s\n' "$output" | grep -q '^TEXT_FINAL .\{1,\}$'
printf '%s\n' "$output" | grep -q '^INSERT_OK target=fake status=inserted$'
