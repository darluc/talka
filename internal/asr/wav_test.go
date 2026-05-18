package asr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealASRSmokeFixtureContainsEnoughSpeech(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "audio", "zh-short.wav"))
	if err != nil {
		t.Fatalf("ReadFile(zh-short.wav) error = %v", err)
	}

	pcm, err := decodePCM16LEWAV(data)
	if err != nil {
		t.Fatalf("decodePCM16LEWAV() error = %v", err)
	}

	const minSpeechSeconds = 4
	const bytesPerSecond = 16000 * 2
	if got, wantAtLeast := len(pcm), minSpeechSeconds*bytesPerSecond; got < wantAtLeast {
		t.Fatalf("zh-short.wav PCM bytes = %d, want at least %d for real ASR smoke input", got, wantAtLeast)
	}
}
