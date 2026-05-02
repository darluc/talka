#!/bin/sh
set -eu

output="$(
  ./scripts/smoke-pairing.sh --pin correct --replay
  ./scripts/smoke-settings.sh --ollama-url http://localhost:11434 --model qwen3:8b --asr-model fixtures/models/fake
)"
printf '%s\n' "$output"

case "$output" in
  *'"pin"'*|*'PIN '*|*'pin:'*|*'send_key'*|*'receive_key'*|*'session_key'*|*'raw_audio'*|*'full_transcript'*|*'RAW_ASR '*|*'TEXT_FINAL '*)
    printf 'privacy log scan found forbidden material\n' >&2
    exit 1
    ;;
esac

printf '%s\n' "$output" | grep -q '^EXPECTED_ERROR replay$'
printf '%s\n' "$output" | grep -q '^SECRET_SCAN_OK$'
printf 'PRIVACY_LOG_SCAN_OK forbidden="pin,key,raw_audio,full_transcript,raw_asr,text_final"\n'
