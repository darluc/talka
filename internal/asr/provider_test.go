package asr

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestSidecarProviderTranscribesFixturePCMFrames(t *testing.T) {
	runtime := &FakeRuntime{Ready: true}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	frames, err := LoadPCMFrames(filepath.Join("..", "..", "fixtures", "audio", "zh-short.pcm"), DefaultFrameSize)
	if err != nil {
		t.Fatalf("LoadPCMFrames() error = %v", err)
	}

	provider := NewSidecarProvider(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	result, err := provider.Transcribe(context.Background(), Request{Metadata: DefaultAudioMetadata(), Frames: frames})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}

	if got, want := result.Transcript, "你好，世界"; got != want {
		t.Fatalf("Transcript = %q, want %q", got, want)
	}
	if got, want := len(result.Partials), 2; got != want {
		t.Fatalf("len(Partials) = %d, want %d", got, want)
	}
	if got, want := len(result.FinalSegments), 1; got != want {
		t.Fatalf("len(FinalSegments) = %d, want %d", got, want)
	}
}
