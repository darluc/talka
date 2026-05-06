package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"talka/internal/protocol"
)

type UpstreamManager interface {
	EnsureRunning(ctx context.Context) error
}

type UpstreamProviderConfig struct {
	URL     string
	Mode    string
	Timeout time.Duration
}

type UpstreamProvider struct {
	manager UpstreamManager
	config  UpstreamProviderConfig
}

func NewUpstreamProvider(manager UpstreamManager, config UpstreamProviderConfig) *UpstreamProvider {
	if strings.TrimSpace(config.Mode) == "" {
		config.Mode = "2pass"
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}
	return &UpstreamProvider{manager: manager, config: config}
}

func (p *UpstreamProvider) Transcribe(ctx context.Context, request Request) (Result, error) {
	if p.manager != nil {
		if err := p.manager.EnsureRunning(ctx); err != nil {
			return Result{}, err
		}
	}

	conn, err := dialFunASRWebSocket(ctx, p.runtimeURL(), p.config.Timeout)
	if err != nil {
		return Result{}, newRuntimeUnavailableError("upstream FunASR runtime unavailable", err)
	}
	defer conn.Close()

	start := funASRStartMessage{
		Mode:       normalizeFunASRMode(p.config.Mode),
		WavName:    defaultStreamID,
		WavFormat:  "pcm",
		IsSpeaking: true,
		ITN:        true,
	}
	if start.Mode == "2pass" {
		start.ChunkSize = []int{5, 10, 5}
	}
	if err := conn.WriteJSON(start); err != nil {
		return Result{}, newRuntimeUnavailableError("upstream FunASR runtime start failed", err)
	}

	result := Result{}
	for _, frame := range request.Frames {
		if err := conn.WriteBinary(frame); err != nil {
			return Result{}, newRuntimeUnavailableError("upstream FunASR write failed", err)
		}
		if err := drainUpstreamResult(ctx, conn, &result); err != nil {
			return Result{}, newRuntimeUnavailableError("upstream FunASR read failed", err)
		}
	}

	if err := conn.WriteJSON(map[string]any{"is_speaking": false}); err != nil {
		return Result{}, newRuntimeUnavailableError("upstream FunASR stop failed", err)
	}
	if err := conn.WriteBinary(nil); err != nil {
		return Result{}, newRuntimeUnavailableError("upstream FunASR finalization flush failed", err)
	}
	if err := awaitFinalResult(ctx, conn, &result); err != nil {
		return Result{}, newRuntimeUnavailableError("upstream FunASR finalization failed", err)
	}

	if result.Transcript == "" {
		result.Transcript = joinFinalSegments(result.FinalSegments)
	}
	if strings.TrimSpace(result.Transcript) == "" {
		return Result{}, newRuntimeError(ErrorCodeEmptyTranscript, "upstream FunASR returned an empty transcript", nil)
	}
	return result, nil
}

func (p *UpstreamProvider) runtimeURL() string {
	type runtimeURLProvider interface {
		URL() string
	}
	if provider, ok := p.manager.(runtimeURLProvider); ok {
		if url := strings.TrimSpace(provider.URL()); url != "" {
			return url
		}
	}
	return p.config.URL
}

func (p *UpstreamProvider) ManagerStartupTimeout() int {
	type startupTimeoutProvider interface {
		StartupTimeout() time.Duration
	}
	if provider, ok := p.manager.(startupTimeoutProvider); ok {
		return int(provider.StartupTimeout().Round(time.Second) / time.Second)
	}
	return 0
}

func normalizeFunASRMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "2pass", "twopass":
		return "2pass"
	case "offline":
		return "offline"
	case "online":
		return "online"
	default:
		return mode
	}
}

func drainUpstreamResult(ctx context.Context, conn *websocketConn, result *Result) error {
	for {
		readCtx, cancel := context.WithTimeout(ctx, upstreamQuietWindow)
		payload, opcode, err := conn.readFrameWithOpcode(readCtx)
		cancel()
		if err != nil {
			if isTimeout(err) {
				return nil
			}
			return err
		}
		if opcode != 0x1 {
			continue
		}
		if err := appendFunASRTranscript(payload, result); err != nil {
			return err
		}
	}
}

func awaitFinalResult(ctx context.Context, conn *websocketConn, result *Result) error {
	deadline := time.Now().Add(upstreamFinalizationTimeout)
	for {
		if time.Now().After(deadline) {
			if result.Transcript != "" || len(result.FinalSegments) > 0 {
				result.Transcript = joinFinalSegments(result.FinalSegments)
				return nil
			}
			return context.DeadlineExceeded
		}

		readCtx, cancel := context.WithTimeout(ctx, upstreamQuietWindow)
		payload, opcode, err := conn.readFrameWithOpcode(readCtx)
		cancel()
		if err != nil {
			if isTimeout(err) {
				if result.Transcript != "" || len(result.FinalSegments) > 0 {
					result.Transcript = joinFinalSegments(result.FinalSegments)
					return nil
				}
				continue
			}
			if errors.Is(err, io.EOF) && (result.Transcript != "" || len(result.FinalSegments) > 0) {
				result.Transcript = joinFinalSegments(result.FinalSegments)
				return nil
			}
			return err
		}
		if opcode != 0x1 {
			continue
		}
		if err := appendFunASRTranscript(payload, result); err != nil {
			return err
		}
	}
}

func appendFunASRTranscript(payload []byte, result *Result) error {
	var response funASRResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return err
	}

	text := strings.TrimSpace(response.Text)
	if text == "" {
		if response.IsFinal {
			return fmt.Errorf("upstream FunASR returned empty final transcript")
		}
		return nil
	}

	if response.IsFinal {
		result.FinalSegments = append(result.FinalSegments, protocol.ASRFinal{
			Envelope:     protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeASRFinal},
			SessionID:    defaultSessionID,
			StreamID:     defaultStreamID,
			SegmentIndex: len(result.FinalSegments) + 1,
			Text:         text,
		})
		result.Transcript = joinFinalSegments(result.FinalSegments)
		return nil
	}

	result.Partials = append(result.Partials, protocol.ASRPartial{
		Envelope:     protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeASRPartial},
		SessionID:    defaultSessionID,
		StreamID:     defaultStreamID,
		SegmentIndex: len(result.Partials) + 1,
		Text:         text,
	})
	return nil
}
