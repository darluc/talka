package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeProviderCleansChineseDictationConservatively(t *testing.T) {
	rawBytes, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "asr", "zh_raw.txt"))
	if err != nil {
		t.Fatalf("ReadFile(raw) error = %v", err)
	}
	cleanBytes, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "llm", "zh_clean.txt"))
	if err != nil {
		t.Fatalf("ReadFile(clean) error = %v", err)
	}

	raw := strings.TrimSpace(string(rawBytes))
	want := strings.TrimSpace(string(cleanBytes))

	provider := NewFakeProvider(FakeConfig{})
	result, err := provider.Cleanup(context.Background(), raw)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if got := result.Status; got != StatusCleaned {
		t.Fatalf("Status = %q, want %q", got, StatusCleaned)
	}
	if got := result.Text; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if got := result.RawTranscript; got != raw {
		t.Fatalf("RawTranscript = %q, want %q", got, raw)
	}
}

func TestFakeProviderReturnsTypedTimeoutFallback(t *testing.T) {
	provider := NewFakeProvider(FakeConfig{Mode: FailureModeTimeout})

	result, err := provider.Cleanup(context.Background(), "你好世界")
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if got := result.Status; got != StatusFallbackTimeout {
		t.Fatalf("Status = %q, want %q", got, StatusFallbackTimeout)
	}
	if got, want := result.Text, "你好世界"; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if got, want := result.RawTranscript, "你好世界"; got != want {
		t.Fatalf("RawTranscript = %q, want %q", got, want)
	}
}
