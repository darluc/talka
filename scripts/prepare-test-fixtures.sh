#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
FIXTURE_DIR="$ROOT_DIR/fixtures/audio"
PCM_PATH="$FIXTURE_DIR/zh-short.pcm"
WAV_PATH="$FIXTURE_DIR/zh-short.wav"

mkdir -p "$FIXTURE_DIR"

if [ -f "$PCM_PATH" ] && [ -f "$WAV_PATH" ]; then
  printf 'AUDIO_FIXTURES_READY=%s\n' "$FIXTURE_DIR"
  exit 0
fi

python3 - "$PCM_PATH" "$WAV_PATH" <<'PY'
import math
import struct
import sys
import wave

pcm_path, wav_path = sys.argv[1], sys.argv[2]
sample_rate = 16000
duration_seconds = 5
frequency_hz = 440
amplitude = 0.18

frames = bytearray()
for i in range(sample_rate * duration_seconds):
    envelope = min(1.0, i / sample_rate / 0.2, (sample_rate * duration_seconds - i) / sample_rate / 0.2)
    sample = int(32767 * amplitude * envelope * math.sin(2 * math.pi * frequency_hz * i / sample_rate))
    frames.extend(struct.pack("<h", sample))

with open(pcm_path, "wb") as f:
    f.write(frames)

with wave.open(wav_path, "wb") as wav:
    wav.setnchannels(1)
    wav.setsampwidth(2)
    wav.setframerate(sample_rate)
    wav.writeframes(frames)
PY

printf 'AUDIO_FIXTURES_READY=%s\n' "$FIXTURE_DIR"
