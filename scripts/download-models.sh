#!/bin/sh
set -eu

if [ "${1-}" = "--verify-only" ] || [ "${1-}" = "" ]; then
  if [ ! -f models/funasr/checksums.txt ]; then
    printf 'missing model checksum manifest: models/funasr/checksums.txt\n' >&2
    exit 1
  fi

  for dir in paraformer-zh-onnx paraformer-zh-online-onnx fsmn-vad-onnx ct-punc-onnx itn-zh; do
    mkdir -p "models/funasr/$dir"
  done

  printf 'Model scaffold verified. Download execution is intentionally deferred in this scaffold task.\n'
  exit 0
fi

printf 'usage: %s [--verify-only]\n' "$0" >&2
exit 2
