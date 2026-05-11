package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"talka/internal/asr"
	"talka/internal/inject"
	"talka/internal/llm"
	"talka/internal/pairing"
	"talka/internal/protocol"
)

type ASRProvider = asr.ASRProvider

type LLMProvider = llm.LLMProvider

type TextInjector = inject.TextInjector

type PairingStore = pairing.Store

type SessionTransport interface {
	Send(ctx context.Context, messageType protocol.MessageType, payload []byte) error
	Receive(ctx context.Context) (protocol.MessageType, []byte, error)
}

type Pipeline struct {
	asr      ASRProvider
	llm      LLMProvider
	injector TextInjector
}

type readinessChecker interface {
	EnsureReady(ctx context.Context) error
}

type healthChecker interface {
	HealthCheck(ctx context.Context) error
}

type shutdowner interface {
	Shutdown(ctx context.Context) error
}

type ProcessResult struct {
	RawTranscript string
	FinalText     string
	Cleanup       llm.Result
	Receipt       inject.Receipt
}

type PipelineTimings struct {
	ASR time.Duration
	LLM time.Duration
}

func NewPipeline(asrProvider ASRProvider, llmProvider LLMProvider, injector TextInjector) *Pipeline {
	return &Pipeline{asr: asrProvider, llm: llmProvider, injector: injector}
}

func (p *Pipeline) EnsureASRReady(ctx context.Context) error {
	if p == nil || p.asr == nil {
		return fmt.Errorf("asr provider is not configured")
	}
	if checker, ok := p.asr.(readinessChecker); ok {
		return checker.EnsureReady(ctx)
	}
	if checker, ok := p.asr.(healthChecker); ok {
		return checker.HealthCheck(ctx)
	}
	return nil
}

func (p *Pipeline) CheckLLM(ctx context.Context) error {
	if p == nil || p.llm == nil {
		return fmt.Errorf("llm provider is not configured")
	}
	if checker, ok := p.llm.(healthChecker); ok {
		return checker.HealthCheck(ctx)
	}
	return nil
}

func (p *Pipeline) Shutdown(ctx context.Context) error {
	if p == nil || p.asr == nil {
		return nil
	}
	if stopper, ok := p.asr.(shutdowner); ok {
		return stopper.Shutdown(ctx)
	}
	return nil
}

func (p *Pipeline) ProcessPCMFile(ctx context.Context, path string) (ProcessResult, error) {
	frames, err := asr.LoadPCMFrames(path, asr.DefaultFrameSize)
	if err != nil {
		return ProcessResult{}, err
	}
	return p.ProcessAudioFrames(ctx, asr.DefaultAudioMetadata(), frames)
}

func (p *Pipeline) ProcessAudioFrames(ctx context.Context, metadata protocol.AudioMetadata, frames [][]byte) (ProcessResult, error) {
	result, err := p.PrepareAudioFrames(ctx, metadata, frames)
	if err != nil {
		return ProcessResult{}, err
	}

	receipt, err := p.InsertText(ctx, result.FinalText)
	if err != nil {
		return result, err
	}

	result.Receipt = receipt
	return result, nil
}

func (p *Pipeline) PrepareAudioFrames(ctx context.Context, metadata protocol.AudioMetadata, frames [][]byte) (ProcessResult, error) {
	result, _, err := p.PrepareAudioFramesWithTimings(ctx, metadata, frames)
	return result, err
}

func (p *Pipeline) PrepareAudioFramesWithTimings(ctx context.Context, metadata protocol.AudioMetadata, frames [][]byte) (ProcessResult, PipelineTimings, error) {
	var timings PipelineTimings
	asrStartedAt := time.Now()
	transcript, err := p.asr.Transcribe(ctx, asr.Request{Metadata: metadata, Frames: frames})
	timings.ASR = time.Since(asrStartedAt)
	if err != nil {
		return ProcessResult{}, timings, err
	}

	rawTranscript := strings.TrimSpace(transcript.Transcript)
	if rawTranscript == "" {
		return ProcessResult{
			RawTranscript: "",
			FinalText:     "",
			Cleanup:       llm.Result{Text: "", RawTranscript: "", Status: llm.StatusCleaned},
		}, timings, nil
	}

	llmStartedAt := time.Now()
	cleanup, err := p.llm.Cleanup(ctx, rawTranscript)
	timings.LLM = time.Since(llmStartedAt)
	if err != nil {
		return ProcessResult{}, timings, err
	}

	return ProcessResult{
		RawTranscript: rawTranscript,
		FinalText:     cleanup.Text,
		Cleanup:       cleanup,
	}, timings, nil
}

func (p *Pipeline) InsertText(ctx context.Context, text string) (inject.Receipt, error) {
	if p.injector == nil {
		return inject.Receipt{}, fmt.Errorf("text injector is not configured")
	}
	return p.injector.Insert(ctx, text)
}
