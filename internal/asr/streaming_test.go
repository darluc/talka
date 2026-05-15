package asr

import (
	"context"
	"reflect"
	"testing"

	"talka/internal/protocol"
)

func TestStreamingBatchAdapterFeedsFramesAndFinishes(t *testing.T) {
	provider := &recordingStreamingProvider{result: Result{Transcript: "你好"}}
	adapter := StreamingBatchAdapter{Provider: provider}

	result, err := adapter.Transcribe(context.Background(), Request{
		Metadata: DefaultAudioMetadata(),
		Frames:   [][]byte{[]byte("frame-1"), []byte("frame-2")},
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if result.Transcript != "你好" {
		t.Fatalf("Transcript = %q, want 你好", result.Transcript)
	}
	if !reflect.DeepEqual(provider.session.frames, [][]byte{[]byte("frame-1"), []byte("frame-2")}) {
		t.Fatalf("frames = %q, want forwarded frames", provider.session.frames)
	}
	if !provider.session.finished {
		t.Fatal("Finish() was not called")
	}
	if !provider.session.closed {
		t.Fatal("Close() was not called")
	}
}

func TestSherpaONNXFinalSilenceSampleCount(t *testing.T) {
	if got := sherpaONNXFinalSilenceSampleCount(16_000); got != 4_800 {
		t.Fatalf("sample count = %d, want 4800", got)
	}
	if got := sherpaONNXFinalSilenceSampleCount(0); got != 0 {
		t.Fatalf("sample count for invalid rate = %d, want 0", got)
	}
}

type recordingStreamingProvider struct {
	result  Result
	session *recordingStreamingSession
}

func (p *recordingStreamingProvider) NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error) {
	p.session = &recordingStreamingSession{result: p.result}
	return p.session, nil
}

type recordingStreamingSession struct {
	frames   [][]byte
	result   Result
	finished bool
	closed   bool
}

func (s *recordingStreamingSession) AcceptFrame(ctx context.Context, frame []byte) (Result, error) {
	s.frames = append(s.frames, append([]byte(nil), frame...))
	return Result{}, nil
}

func (s *recordingStreamingSession) Finish(ctx context.Context) (Result, error) {
	s.finished = true
	return s.result, nil
}

func (s *recordingStreamingSession) Close(ctx context.Context) error {
	s.closed = true
	return nil
}
