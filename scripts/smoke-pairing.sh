#!/bin/sh
set -eu

pin_mode="correct"
replay_mode="false"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pin)
      shift
      pin_mode="${1:-}"
      ;;
    --replay)
      replay_mode="true"
      ;;
    *)
      printf 'usage: %s [--pin correct|wrong] [--replay]\n' "$0" >&2
      exit 2
      ;;
  esac
  shift
done

replay_args=""
if [ "$replay_mode" = "true" ]; then
  replay_args=" --replay"
fi
output="$(go run ./internal/pairing/smokepairing --pin "$pin_mode"$replay_args)"
printf '%s\n' "$output"

if [ "$pin_mode" = "correct" ]; then
  printf '%s\n' "$output" | grep -q '^PAIRING_OK '
  printf '%s\n' "$output" | grep -q '^SESSION_OK smoke$'
  printf '%s\n' "$output" | grep -q '^RECONNECT_OK$'
else
  printf '%s\n' "$output" | grep -q '^EXPECTED_ERROR invalid_pin$'
fi

if [ "$replay_mode" = "true" ]; then
  printf '%s\n' "$output" | grep -q '^EXPECTED_ERROR replay$'
fi
