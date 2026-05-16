package asr

import (
	"context"
	"os"
	"strings"
	"time"

	"talka/internal/protocol"
)

const DefaultFrameSize = 640

type Request struct {
	Metadata protocol.AudioMetadata
	Frames   [][]byte
}

type Result struct {
	Partials      []protocol.ASRPartial
	FinalSegments []protocol.ASRFinal
	Transcript    string
}

type ASRProvider interface {
	Transcribe(ctx context.Context, request Request) (Result, error)
}

type StreamingProvider interface {
	NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error)
}

type StreamingSession interface {
	AcceptFrame(ctx context.Context, frame []byte) (Result, error)
	Finish(ctx context.Context) (Result, error)
	Close(ctx context.Context) error
}

type StreamingBatchAdapter struct {
	Provider StreamingProvider
}

func (a StreamingBatchAdapter) Transcribe(ctx context.Context, request Request) (Result, error) {
	stream, err := a.Provider.NewStream(ctx, request.Metadata)
	if err != nil {
		return Result{}, err
	}
	defer stream.Close(ctx)

	for _, frame := range request.Frames {
		if _, err := stream.AcceptFrame(ctx, frame); err != nil {
			return Result{}, err
		}
	}
	return stream.Finish(ctx)
}

type SidecarProvider struct {
	client *Client
}

type SidecarRuntimeManager interface {
	EnsureRunning(ctx context.Context) error
	URL() string
}

type ManagedSidecarProvider struct {
	manager      SidecarRuntimeManager
	clientConfig Config
}

func NewSidecarProvider(config Config) *SidecarProvider {
	return &SidecarProvider{client: NewClient(config)}
}

func NewManagedSidecarProvider(manager SidecarRuntimeManager, config Config) *ManagedSidecarProvider {
	return &ManagedSidecarProvider{manager: manager, clientConfig: config}
}

func (p *SidecarProvider) Transcribe(ctx context.Context, request Request) (Result, error) {
	stream, err := p.client.Transcribe(ctx, request.Metadata, request.Frames)
	if err != nil {
		return Result{}, err
	}

	return resultFromStreamResult(stream), nil
}

func (p *SidecarProvider) NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error) {
	stream, err := p.client.NewStream(ctx, metadata)
	if err != nil {
		return nil, err
	}
	return &sidecarStreamingSession{stream: stream}, nil
}

type sidecarStreamingSession struct {
	stream *ClientStream
}

func (s *sidecarStreamingSession) AcceptFrame(ctx context.Context, frame []byte) (Result, error) {
	streamResult, err := s.stream.AcceptFrame(ctx, 0, frame)
	if err != nil {
		return Result{}, err
	}
	return resultFromStreamResult(streamResult), nil
}

func (s *sidecarStreamingSession) Finish(ctx context.Context) (Result, error) {
	streamResult, err := s.stream.Finish(ctx)
	if err != nil {
		return Result{}, err
	}
	return resultFromStreamResult(streamResult), nil
}

func (s *sidecarStreamingSession) Close(ctx context.Context) error {
	return s.stream.Close(ctx)
}

func resultFromStreamResult(stream StreamResult) Result {
	transcript := joinFinalSegments(stream.Finals)
	if transcript == "" {
		transcript = strings.TrimSpace(stream.TextFinal.Text)
	}

	return Result{
		Partials:      append([]protocol.ASRPartial(nil), stream.Partials...),
		FinalSegments: append([]protocol.ASRFinal(nil), stream.Finals...),
		Transcript:    transcript,
	}
}

func (p *SidecarProvider) HealthCheck(ctx context.Context) error {
	return p.client.HealthCheck(ctx)
}

func (p *ManagedSidecarProvider) Transcribe(ctx context.Context, request Request) (Result, error) {
	if err := p.EnsureReady(ctx); err != nil {
		return Result{}, err
	}
	return NewSidecarProvider(p.runtimeConfig()).Transcribe(ctx, request)
}

func (p *ManagedSidecarProvider) NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error) {
	if err := p.EnsureReady(ctx); err != nil {
		return nil, err
	}
	return NewSidecarProvider(p.runtimeConfig()).NewStream(ctx, metadata)
}

func (p *ManagedSidecarProvider) EnsureReady(ctx context.Context) error {
	if p.manager == nil {
		return NewClient(p.runtimeConfig()).HealthCheck(ctx)
	}
	if err := p.manager.EnsureRunning(ctx); err != nil {
		return err
	}
	return NewClient(p.runtimeConfig()).HealthCheck(ctx)
}

func (p *ManagedSidecarProvider) HealthCheck(ctx context.Context) error {
	return p.EnsureReady(ctx)
}

func (p *ManagedSidecarProvider) Shutdown(ctx context.Context) error {
	type stopper interface {
		Stop(ctx context.Context) error
	}
	if p.manager == nil {
		return nil
	}
	if manager, ok := p.manager.(stopper); ok {
		return manager.Stop(ctx)
	}
	return nil
}

func (p *ManagedSidecarProvider) ManagerStartupTimeout() int {
	type startupTimeoutProvider interface {
		StartupTimeout() time.Duration
	}
	if provider, ok := p.manager.(startupTimeoutProvider); ok {
		return int(provider.StartupTimeout().Round(time.Second) / time.Second)
	}
	return 0
}

func (p *ManagedSidecarProvider) ManagerAlwaysEphemeral() bool {
	type alwaysEphemeralProvider interface {
		AlwaysEphemeral() bool
	}
	if provider, ok := p.manager.(alwaysEphemeralProvider); ok {
		return provider.AlwaysEphemeral()
	}
	return false
}

func (p *ManagedSidecarProvider) runtimeConfig() Config {
	config := p.clientConfig
	if p.manager != nil {
		if url := strings.TrimSpace(p.manager.URL()); url != "" {
			config.URL = url
		}
	}
	return config
}

func DefaultAudioMetadata() protocol.AudioMetadata {
	return protocol.AudioMetadata{
		SampleRate:      16000,
		Channels:        1,
		Encoding:        protocol.EncodingPCMS16LE,
		FrameDurationMS: 20,
		Language:        "zh-CN",
	}
}

func LoadPCMFrames(path string, frameSize int) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return splitPCMFrames(data, frameSize), nil
}

func splitPCMFrames(data []byte, frameSize int) [][]byte {
	if frameSize <= 0 {
		frameSize = DefaultFrameSize
	}

	frames := make([][]byte, 0, (len(data)+frameSize-1)/frameSize)
	for len(data) > 0 {
		next := frameSize
		if len(data) < frameSize {
			next = len(data)
		}
		frame := make([]byte, next)
		copy(frame, data[:next])
		frames = append(frames, frame)
		data = data[next:]
	}
	return frames
}

func joinFinalSegments(finals []protocol.ASRFinal) string {
	if len(finals) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, final := range finals {
		builder.WriteString(final.Text)
	}
	return strings.TrimSpace(builder.String())
}
