package asr

import (
	"context"
	"os"
	"strings"

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

type SidecarProvider struct {
	client *Client
}

func NewSidecarProvider(config Config) *SidecarProvider {
	return &SidecarProvider{client: NewClient(config)}
}

func (p *SidecarProvider) Transcribe(ctx context.Context, request Request) (Result, error) {
	stream, err := p.client.Transcribe(ctx, request.Metadata, request.Frames)
	if err != nil {
		return Result{}, err
	}

	transcript := joinFinalSegments(stream.Finals)
	if transcript == "" {
		transcript = strings.TrimSpace(stream.TextFinal.Text)
	}

	return Result{
		Partials:      append([]protocol.ASRPartial(nil), stream.Partials...),
		FinalSegments: append([]protocol.ASRFinal(nil), stream.Finals...),
		Transcript:    transcript,
	}, nil
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
