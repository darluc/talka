#!/bin/sh
set -eu

config_path="${1:-fixtures/config/default.sample.yaml}"

if [ ! -f "$config_path" ]; then
  printf 'usage: %s [config-path]\n' "$0" >&2
  printf 'missing config file: %s\n' "$config_path" >&2
  exit 2
fi

server_log="$(mktemp)"
invalid_log="$(mktemp)"
cleanup() {
  if [ "${server_pid-}" != "" ]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -f "$server_log" "$invalid_log"
}
trap cleanup EXIT INT TERM

go run ./cmd/talka-server --config "$config_path" --listen 127.0.0.1:0 >"$server_log" 2>&1 &
server_pid=$!

attempt=0
while [ "$attempt" -lt 100 ]; do
  if grep -q '^LISTEN http://' "$server_log"; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.1
done

if ! grep -q '^LISTEN http://' "$server_log"; then
  printf 'server did not print a LISTEN line\n' >&2
  cat "$server_log" >&2
  exit 1
fi

base_url="$(grep '^LISTEN http://' "$server_log" | tail -n 1 | sed 's/^LISTEN //')"
status_json="$(curl -fsS "$base_url/v1/status")"
printf '%s\n' "$status_json"
printf '%s\n' "$status_json" | grep -q '"service_name":"Talka"'
printf '%s\n' "$status_json" | grep -q '"asr":{'
printf '%s\n' "$status_json" | grep -q '"ollama":{'
printf '%s\n' "$status_json" | grep -q '"pairing_active":'
printf '%s\n' "$status_json" | grep -q '"permissions":{'

invalid_config="$(mktemp -d)/missing-config.yaml"
if go run ./cmd/talka-server --config "$invalid_config" --listen 127.0.0.1:0 >"$invalid_log" 2>&1; then
  printf 'expected invalid config path to fail\n' >&2
  exit 1
fi

if ! grep -Eq 'open config|config' "$invalid_log"; then
  printf 'invalid config failure was not actionable:\n' >&2
  cat "$invalid_log" >&2
  exit 1
fi
