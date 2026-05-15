package asr

import (
	"context"
	"fmt"
	"math"
	"strings"

	"talka/internal/protocol"
)

const sherpaONNXFinalSilenceMS = 300

type SherpaONNXConfig struct {
	ModelType      string
	Precision      string
	TokensPath     string
	EncoderPath    string
	DecoderPath    string
	JoinerPath     string
	NumThreads     int
	DecodingMethod string
	FeatureDim     int
	Provider       string
}

type SherpaONNXProvider struct {
	impl sherpaONNXImpl
}

type sherpaONNXImpl interface {
	NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error)
	Close(ctx context.Context) error
}

func NewSherpaONNXProvider(config SherpaONNXConfig) (*SherpaONNXProvider, error) {
	config = normalizeSherpaONNXConfig(config)
	impl, err := newSherpaONNXImpl(config)
	if err != nil {
		return nil, err
	}
	return &SherpaONNXProvider{impl: impl}, nil
}

func (p *SherpaONNXProvider) Transcribe(ctx context.Context, request Request) (Result, error) {
	return StreamingBatchAdapter{Provider: p}.Transcribe(ctx, request)
}

func (p *SherpaONNXProvider) NewStream(ctx context.Context, metadata protocol.AudioMetadata) (StreamingSession, error) {
	if p == nil || p.impl == nil {
		return nil, fmt.Errorf("sherpa-onnx provider is not configured")
	}
	return p.impl.NewStream(ctx, metadata)
}

func (p *SherpaONNXProvider) Shutdown(ctx context.Context) error {
	if p == nil || p.impl == nil {
		return nil
	}
	return p.impl.Close(ctx)
}

func normalizeSherpaONNXConfig(config SherpaONNXConfig) SherpaONNXConfig {
	if strings.TrimSpace(config.ModelType) == "" {
		if strings.TrimSpace(config.JoinerPath) != "" {
			config.ModelType = "transducer"
		} else {
			config.ModelType = "paraformer"
		}
	}
	if strings.TrimSpace(config.Precision) == "" {
		config.Precision = "int8"
	}
	if config.NumThreads <= 0 {
		config.NumThreads = 2
	}
	if strings.TrimSpace(config.DecodingMethod) == "" {
		config.DecodingMethod = "greedy_search"
	}
	if config.FeatureDim <= 0 {
		config.FeatureDim = 80
	}
	if strings.TrimSpace(config.Provider) == "" {
		config.Provider = "cpu"
	}
	return config
}

func sherpaONNXFinalSilenceSampleCount(sampleRate int) int {
	if sampleRate <= 0 {
		return 0
	}
	return int(math.Round(float64(sampleRate) * float64(sherpaONNXFinalSilenceMS) / 1000.0))
}
