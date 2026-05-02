package app

import (
	"context"

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

type ProcessResult struct {
	RawTranscript string
	FinalText     string
	Cleanup       llm.Result
	Receipt       inject.Receipt
}

func NewPipeline(asrProvider ASRProvider, llmProvider LLMProvider, injector TextInjector) *Pipeline {
	return &Pipeline{asr: asrProvider, llm: llmProvider, injector: injector}
}

func (p *Pipeline) ProcessPCMFile(ctx context.Context, path string) (ProcessResult, error) {
	frames, err := asr.LoadPCMFrames(path, asr.DefaultFrameSize)
	if err != nil {
		return ProcessResult{}, err
	}
	return p.ProcessAudioFrames(ctx, asr.DefaultAudioMetadata(), frames)
}

func (p *Pipeline) ProcessAudioFrames(ctx context.Context, metadata protocol.AudioMetadata, frames [][]byte) (ProcessResult, error) {
	transcript, err := p.asr.Transcribe(ctx, asr.Request{Metadata: metadata, Frames: frames})
	if err != nil {
		return ProcessResult{}, err
	}

	cleanup, err := p.llm.Cleanup(ctx, transcript.Transcript)
	if err != nil {
		return ProcessResult{}, err
	}

	receipt, err := p.injector.Insert(ctx, cleanup.Text)
	if err != nil {
		return ProcessResult{
			RawTranscript: transcript.Transcript,
			FinalText:     cleanup.Text,
			Cleanup:       cleanup,
		}, err
	}

	return ProcessResult{
		RawTranscript: transcript.Transcript,
		FinalText:     cleanup.Text,
		Cleanup:       cleanup,
		Receipt:       receipt,
	}, nil
}
