#!/bin/sh
set -eu

fixture=""
base_url="${TALKA_OLLAMA_BASE_URL:-http://localhost:11434}"
model="${TALKA_OLLAMA_MODEL:-qwen3:8b}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fixture)
      if [ "$#" -lt 2 ]; then
        printf 'usage: %s --fixture <path>\n' "$0" >&2
        exit 2
      fi
      fixture="$2"
      shift 2
      ;;
    *)
      printf 'usage: %s --fixture <path>\n' "$0" >&2
      exit 2
      ;;
  esac
done

if [ -z "$fixture" ]; then
  printf 'usage: %s --fixture <path>\n' "$0" >&2
  exit 2
fi

if [ ! -f "$fixture" ]; then
  printf 'missing fixture: %s\n' "$fixture" >&2
  exit 1
fi

if ! curl -fsS "$base_url/api/tags" >/dev/null 2>&1; then
  printf 'Ollama is not reachable at %s. Start it locally (for example: `ollama serve`) and ensure model %s is available.\n' "$base_url" "$model" >&2
  exit 1
fi

go run ./internal/llm/smokeollama --fixture "$fixture" --base-url "$base_url" --model "$model"
