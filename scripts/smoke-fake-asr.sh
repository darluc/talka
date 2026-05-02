#!/bin/sh
set -eu

fixture=""
sample_rate="16000"
default_port=$((20000 + ($$ % 10000)))
addr="${TALKA_FAKE_ASR_ADDR:-127.0.0.1:${default_port}}"
url="ws://${addr}/ws"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fixture)
      if [ "$#" -lt 2 ]; then
        printf 'usage: %s --fixture <path> [--sample-rate 16000]\n' "$0" >&2
        exit 2
      fi
      fixture="$2"
      shift 2
      ;;
    --sample-rate)
      if [ "$#" -lt 2 ]; then
        printf 'usage: %s --fixture <path> [--sample-rate 16000]\n' "$0" >&2
        exit 2
      fi
      sample_rate="$2"
      shift 2
      ;;
    *)
      printf 'usage: %s --fixture <path> [--sample-rate 16000]\n' "$0" >&2
      exit 2
      ;;
  esac
done

if [ -z "$fixture" ]; then
  printf 'usage: %s --fixture <path> [--sample-rate 16000]\n' "$0" >&2
  exit 2
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

go run ./cmd/talka-fake-asr serve --addr "$addr" >"$log_file" 2>&1 &
server_pid=$!

attempt=0
while [ "$attempt" -lt 50 ]; do
  if go run ./cmd/talka-fake-asr health --url "$url" >/dev/null 2>&1; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.1
done

if ! go run ./cmd/talka-fake-asr health --url "$url" >/dev/null 2>&1; then
  printf 'fake ASR sidecar did not become healthy\n' >&2
  cat "$log_file" >&2
  exit 1
fi

go run ./cmd/talka-fake-asr smoke --fixture "$fixture" --url "$url" --sample-rate "$sample_rate"
