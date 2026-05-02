#!/bin/sh
set -eu

fixture=""
default_port=$((21000 + ($$ % 10000)))
addr="${TALKA_REAL_ASR_ADDR:-127.0.0.1:${default_port}}"
url="ws://${addr}/ws"
upstream_url="${TALKA_FUNASR_UPSTREAM_URL:-ws://127.0.0.1:10095}"
model_asr="${TALKA_FUNASR_MODEL_ASR:-models/funasr/paraformer-zh-onnx}"
model_vad="${TALKA_FUNASR_MODEL_VAD:-models/funasr/fsmn-vad-onnx}"
model_punc="${TALKA_FUNASR_MODEL_PUNC:-models/funasr/ct-punc-onnx}"
model_itn="${TALKA_FUNASR_MODEL_ITN:-models/funasr/itn-zh}"
crash_midstream=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fixture)
      if [ "$#" -lt 2 ]; then
        printf 'usage: %s --fixture <path> [--crash-midstream]\n' "$0" >&2
        exit 2
      fi
      fixture="$2"
      shift 2
      ;;
    --crash-midstream)
      crash_midstream=1
      shift
      ;;
    *)
      printf 'usage: %s --fixture <path> [--crash-midstream]\n' "$0" >&2
      exit 2
      ;;
  esac
done

if [ -z "$fixture" ]; then
  printf 'usage: %s --fixture <path> [--crash-midstream]\n' "$0" >&2
  exit 2
fi

if [ ! -f "$fixture" ]; then
  printf 'missing fixture: %s\n' "$fixture" >&2
  exit 1
fi

if [ "$crash_midstream" -eq 0 ]; then
  for path in "$model_asr" "$model_vad" "$model_punc" "$model_itn"; do
    if [ ! -d "$path" ]; then
      printf 'missing FunASR model path: %s. Download or point TALKA_FUNASR_MODEL_* at local ONNX model directories before running this smoke test.\n' "$path" >&2
      exit 1
    fi
    if [ -z "$(find "$path" -type f ! -name '.gitkeep' -print -quit)" ]; then
      printf 'FunASR model path has no real model assets: %s. Replace scaffold placeholders with local ONNX runtime files or point TALKA_FUNASR_MODEL_* at populated model directories.\n' "$path" >&2
      exit 1
    fi
  done
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

if [ "$crash_midstream" -eq 1 ]; then
  go run ./cmd/talka-asr-runtime serve --addr "$addr" --upstream-url "$upstream_url" --crash-midstream >"$log_file" 2>&1 &
else
  go run ./cmd/talka-asr-runtime serve --addr "$addr" --upstream-url "$upstream_url" >"$log_file" 2>&1 &
fi
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
  if [ "$crash_midstream" -eq 1 ]; then
    printf 'crash-midstream QA sidecar did not become healthy on %s.\n' "$url" >&2
  else
    printf 'real ASR sidecar did not become healthy. Ensure a FunASR websocket runtime is available at %s and reachable from localhost.\n' "$upstream_url" >&2
  fi
  cat "$log_file" >&2
  exit 1
fi

if [ "$crash_midstream" -eq 1 ]; then
  go run ./cmd/talka-asr-runtime smoke --fixture "$fixture" --url "$url" --crash-midstream
else
  go run ./cmd/talka-asr-runtime smoke --fixture "$fixture" --url "$url"
fi
