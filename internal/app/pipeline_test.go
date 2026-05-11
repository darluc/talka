package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"talka/internal/asr"
	"talka/internal/inject"
	"talka/internal/llm"
	"talka/internal/protocol"
)

func TestPipelineProcessesFixtureThroughFakeProviders(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	provider := asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	pipeline := NewPipeline(provider, llm.NewFakeProvider(llm.FakeConfig{}), inject.NewFakeInjector())

	result, err := pipeline.ProcessPCMFile(context.Background(), filepath.Join("..", "..", "fixtures", "audio", "zh-short.pcm"))
	if err != nil {
		t.Fatalf("ProcessPCMFile() error = %v", err)
	}

	if got, want := result.RawTranscript, "你好，世界"; got != want {
		t.Fatalf("RawTranscript = %q, want %q", got, want)
	}
	if got, want := result.FinalText, "你好，世界"; got != want {
		t.Fatalf("FinalText = %q, want %q", got, want)
	}
	if got, want := result.Cleanup.Status, llm.StatusCleaned; got != want {
		t.Fatalf("Cleanup.Status = %q, want %q", got, want)
	}
	if got, want := result.Receipt.Target, "fake"; got != want {
		t.Fatalf("Receipt.Target = %q, want %q", got, want)
	}
	if got, want := result.Receipt.Status, "inserted"; got != want {
		t.Fatalf("Receipt.Status = %q, want %q", got, want)
	}
}

func TestPipelinePreservesRawTranscriptWhenLLMTimesOut(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	provider := asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	pipeline := NewPipeline(provider, llm.NewFakeProvider(llm.FakeConfig{Mode: llm.FailureModeTimeout}), inject.NewFakeInjector())

	result, err := pipeline.ProcessPCMFile(context.Background(), filepath.Join("..", "..", "fixtures", "audio", "zh-short.pcm"))
	if err != nil {
		t.Fatalf("ProcessPCMFile() error = %v", err)
	}

	if got, want := result.Cleanup.Status, llm.StatusFallbackTimeout; got != want {
		t.Fatalf("Cleanup.Status = %q, want %q", got, want)
	}
	if result.FinalText != result.RawTranscript {
		t.Fatalf("FinalText = %q, want raw transcript %q", result.FinalText, result.RawTranscript)
	}
	if got, want := result.Receipt.Target, "fake"; got != want {
		t.Fatalf("Receipt.Target = %q, want %q", got, want)
	}
	if got, want := result.Receipt.Status, "inserted"; got != want {
		t.Fatalf("Receipt.Status = %q, want %q", got, want)
	}
}

func TestPipelineProcessesInMemoryAudioFrames(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	provider := asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	pipeline := NewPipeline(provider, llm.NewFakeProvider(llm.FakeConfig{}), inject.NewFakeInjector())

	result, err := pipeline.ProcessAudioFrames(context.Background(), asr.DefaultAudioMetadata(), [][]byte{
		[]byte("frame-1"),
		[]byte("frame-2"),
	})
	if err != nil {
		t.Fatalf("ProcessAudioFrames() error = %v", err)
	}
	if got, want := result.RawTranscript, "你好，世界"; got != want {
		t.Fatalf("RawTranscript = %q, want %q", got, want)
	}
	if got, want := result.FinalText, "你好，世界"; got != want {
		t.Fatalf("FinalText = %q, want %q", got, want)
	}
}

func TestPipelinePreservesFinalTextWhenInsertionFails(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	provider := asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	pipeline := NewPipeline(provider, llm.NewFakeProvider(llm.FakeConfig{}), failingInjector{err: errors.New("paste denied")})

	result, err := pipeline.ProcessAudioFrames(context.Background(), asr.DefaultAudioMetadata(), [][]byte{
		[]byte("frame-1"),
		[]byte("frame-2"),
	})
	if err == nil {
		t.Fatal("ProcessAudioFrames() error = nil, want insertion error")
	}
	if got, want := result.RawTranscript, "你好，世界"; got != want {
		t.Fatalf("RawTranscript = %q, want %q", got, want)
	}
	if got, want := result.FinalText, "你好，世界"; got != want {
		t.Fatalf("FinalText = %q, want %q", got, want)
	}
}

func TestPipelinePrepareAudioFramesSkipsInsertionUntilRequested(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	injector := &recordingInjector{}
	provider := asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	pipeline := NewPipeline(provider, llm.NewFakeProvider(llm.FakeConfig{}), injector)

	result, err := pipeline.PrepareAudioFrames(context.Background(), asr.DefaultAudioMetadata(), [][]byte{
		[]byte("frame-1"),
		[]byte("frame-2"),
	})
	if err != nil {
		t.Fatalf("PrepareAudioFrames() error = %v", err)
	}
	if got, want := result.FinalText, "你好，世界"; got != want {
		t.Fatalf("FinalText = %q, want %q", got, want)
	}
	if injector.calls != 0 {
		t.Fatalf("Insert() calls = %d, want 0 before explicit insert", injector.calls)
	}

	receipt, err := pipeline.InsertText(context.Background(), result.FinalText)
	if err != nil {
		t.Fatalf("InsertText() error = %v", err)
	}
	if injector.calls != 1 {
		t.Fatalf("Insert() calls = %d, want 1 after explicit insert", injector.calls)
	}
	if got, want := injector.text, "你好，世界"; got != want {
		t.Fatalf("Insert() text = %q, want %q", got, want)
	}
	if got, want := receipt.Target, "recording"; got != want {
		t.Fatalf("Receipt.Target = %q, want %q", got, want)
	}
}

func TestPipelinePrepareAudioFramesWithTimingsReportsASRAndLLMDurations(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	provider := asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	pipeline := NewPipeline(provider, llm.NewFakeProvider(llm.FakeConfig{}), inject.NewFakeInjector())

	result, timings, err := pipeline.PrepareAudioFramesWithTimings(context.Background(), asr.DefaultAudioMetadata(), [][]byte{[]byte("frame-1")})
	if err != nil {
		t.Fatalf("PrepareAudioFramesWithTimings() error = %v", err)
	}
	if result.RawTranscript == "" || result.FinalText == "" {
		t.Fatalf("result = %+v, want transcript and final text", result)
	}
	if timings.ASR <= 0 {
		t.Fatalf("timings.ASR = %v, want positive duration", timings.ASR)
	}
	if timings.LLM <= 0 {
		t.Fatalf("timings.LLM = %v, want positive duration", timings.LLM)
	}
}

func TestPipelinePrepareAudioFramesSkipsLLMWhenASRTranscriptIsEmpty(t *testing.T) {
	llmProvider := &countingLLMProvider{}
	pipeline := NewPipeline(emptyASRProvider{}, llmProvider, inject.NewFakeInjector())

	result, timings, err := pipeline.PrepareAudioFramesWithTimings(context.Background(), asr.DefaultAudioMetadata(), [][]byte{[]byte("frame-1")})
	if err != nil {
		t.Fatalf("PrepareAudioFramesWithTimings() error = %v", err)
	}
	if llmProvider.calls != 0 {
		t.Fatalf("LLM Cleanup calls = %d, want 0 for empty transcript", llmProvider.calls)
	}
	if result.RawTranscript != "" || result.FinalText != "" {
		t.Fatalf("result = %+v, want empty transcript and final text", result)
	}
	if timings.ASR <= 0 {
		t.Fatalf("timings.ASR = %v, want positive duration", timings.ASR)
	}
	if timings.LLM != 0 {
		t.Fatalf("timings.LLM = %v, want 0 when LLM is skipped", timings.LLM)
	}
}

type failingInjector struct {
	err error
}

func (i failingInjector) Insert(context.Context, string) (inject.Receipt, error) {
	return inject.Receipt{}, i.err
}

type recordingInjector struct {
	calls int
	text  string
}

type emptyASRProvider struct{}

func (emptyASRProvider) Transcribe(context.Context, asr.Request) (asr.Result, error) {
	return asr.Result{}, nil
}

type countingLLMProvider struct {
	calls int
}

func (p *countingLLMProvider) Cleanup(_ context.Context, transcript string) (llm.Result, error) {
	p.calls++
	return llm.Result{Text: transcript, RawTranscript: transcript, Status: llm.StatusCleaned, Provider: "counting"}, nil
}

func (i *recordingInjector) Insert(_ context.Context, text string) (inject.Receipt, error) {
	i.calls++
	i.text = text
	return inject.Receipt{Target: "recording", Status: "inserted"}, nil
}
