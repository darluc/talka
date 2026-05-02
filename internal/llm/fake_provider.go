package llm

import (
	"context"
	"strings"
)

type CleanupStatus string

const (
	StatusCleaned         CleanupStatus = "cleaned"
	StatusFallbackTimeout CleanupStatus = "fallback_timeout"
	StatusFallbackError   CleanupStatus = "fallback_error"
)

type FailureMode string

const (
	FailureModeNone    FailureMode = ""
	FailureModeTimeout FailureMode = "timeout"
	FailureModeError   FailureMode = "error"
)

type Result struct {
	Text          string
	RawTranscript string
	Status        CleanupStatus
	Provider      string
}

type LLMProvider interface {
	Cleanup(ctx context.Context, transcript string) (Result, error)
}

type FakeConfig struct {
	Mode FailureMode
}

type FakeProvider struct {
	config FakeConfig
}

func NewFakeProvider(config FakeConfig) *FakeProvider {
	return &FakeProvider{config: config}
}

func (p *FakeProvider) Cleanup(_ context.Context, transcript string) (Result, error) {
	raw := strings.TrimSpace(transcript)

	switch p.config.Mode {
	case FailureModeTimeout:
		return Result{Text: raw, RawTranscript: raw, Status: StatusFallbackTimeout, Provider: "fake-ollama"}, nil
	case FailureModeError:
		return Result{Text: raw, RawTranscript: raw, Status: StatusFallbackError, Provider: "fake-ollama"}, nil
	default:
		return Result{Text: cleanDictation(raw), RawTranscript: raw, Status: StatusCleaned, Provider: "fake-ollama"}, nil
	}
}

func cleanDictation(transcript string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(transcript)), " ")
	switch normalized {
	case "今天天气不错我们下午两点开会":
		return "今天天气不错，我们下午两点开会。"
	default:
		return normalized
	}
}
