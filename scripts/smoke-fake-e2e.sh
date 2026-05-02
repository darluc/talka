#!/bin/sh
set -eu

fixture="fixtures/audio/zh-short.pcm"
llm_timeout="false"
full_session="false"
asr_unavailable="false"
insertion_failure="false"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fixture)
      shift
      fixture="${1:-}"
      ;;
    --llm-timeout)
      llm_timeout="true"
      ;;
    --full-session)
      full_session="true"
      ;;
    --asr-unavailable)
      asr_unavailable="true"
      ;;
    --insertion-failure)
      insertion_failure="true"
      ;;
    *)
      printf 'usage: %s [--fixture path] [--llm-timeout] [--full-session] [--asr-unavailable] [--insertion-failure]\n' "$0" >&2
      exit 2
      ;;
  esac
  shift
done

args="--fixture $fixture"
if [ "$llm_timeout" = "true" ]; then
  args="$args --llm-timeout"
fi
if [ "$full_session" = "true" ]; then
  args="$args --full-session"
fi
if [ "$asr_unavailable" = "true" ]; then
  args="$args --asr-unavailable"
fi
if [ "$insertion_failure" = "true" ]; then
  args="$args --insertion-failure"
fi

# shellcheck disable=SC2086
if output="$(go run ./internal/app/smokefakee2e $args)"; then
  status=0
else
  status=$?
fi
printf '%s\n' "$output"

if [ "$asr_unavailable" = "true" ]; then
  printf '%s\n' "$output" | grep -q '^EXPECTED_ERROR stage=asr '
  if printf '%s\n' "$output" | grep -q '^INSERT_OK '; then
    printf 'ASR failure must not insert text\n' >&2
    exit 1
  fi
  exit 0
fi

if [ "$insertion_failure" = "true" ]; then
  printf '%s\n' "$output" | grep -q '^EXPECTED_ERROR stage=insertion '
  printf '%s\n' "$output" | grep -q '^RECOVERABLE_TEXT 你好，世界$'
  exit 0
fi

if [ "$status" -ne 0 ]; then
  exit "$status"
fi

if [ "$full_session" = "true" ]; then
  printf '%s\n' "$output" | grep -q '^ENCRYPTED_AUDIO_START sample_rate=16000 channels=1 encoding=pcm_s16le frame_ms=20$'
  printf '%s\n' "$output" | grep -q '^ENCRYPTED_AUDIO_FRAME count=[1-9][0-9]*$'
  printf '%s\n' "$output" | grep -q '^ENCRYPTED_AUDIO_STOP last_sequence=[1-9][0-9]*$'
fi

printf '%s\n' "$output" | grep -q '^RAW_ASR 你好，世界$'
printf '%s\n' "$output" | grep -q '^TEXT_FINAL 你好，世界$'
printf '%s\n' "$output" | grep -q '^INSERT_OK target=fake status=inserted$'

if [ "$llm_timeout" = "true" ]; then
  printf '%s\n' "$output" | grep -q '^CLEANUP_STATUS fallback_timeout$'
else
  printf '%s\n' "$output" | grep -q '^CLEANUP_STATUS cleaned$'
fi
