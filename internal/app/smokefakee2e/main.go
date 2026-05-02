package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"talka/internal/app"
	"talka/internal/asr"
	"talka/internal/inject"
	"talka/internal/llm"
	"talka/internal/protocol"
	"talka/internal/session"
)

func main() {
	if err := run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "smokefakee2e: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("smokefakee2e", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fixturePath := fs.String("fixture", "", "path to PCM fixture")
	fixtureFormat := fs.String("fixture-format", "pcm", "fixture format: pcm or wav")
	llmTimeout := fs.Bool("llm-timeout", false, "force fake LLM timeout fallback")
	fullSession := fs.Bool("full-session", false, "exercise encrypted audio_start/frame/stop session before pipeline processing")
	hostIntegration := fs.Bool("host-integration", false, "label run as host-only integration evidence, not physical iOS QA")
	asrURL := fs.String("asr-url", "", "external ASR sidecar websocket URL")
	asrTimeout := fs.Duration("asr-timeout", 5*time.Second, "ASR sidecar timeout")
	llmProvider := fs.String("llm-provider", "fake", "cleanup provider: fake or ollama")
	ollamaBaseURL := fs.String("ollama-base-url", "", "Ollama base URL")
	ollamaModel := fs.String("ollama-model", "", "Ollama model")
	ollamaTimeout := fs.Duration("ollama-timeout", 30*time.Second, "Ollama cleanup timeout")
	debugWAV := fs.String("debug-wav", "", "write reconstructed 16kHz mono PCM WAV from encrypted full-session messages")
	asrUnavailable := fs.Bool("asr-unavailable", false, "force fake ASR runtime unavailable error")
	insertionFailure := fs.Bool("insertion-failure", false, "force fake insertion failure after final cleanup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fixturePath == "" {
		return errors.New("missing required --fixture path")
	}

	providerLabel := "fake"
	selectedASRURL := *asrURL
	shutdown := func() {}
	if selectedASRURL == "" {
		var err error
		selectedASRURL, shutdown, err = startFakeRuntime(ctx, !*asrUnavailable)
		if err != nil {
			return err
		}
		defer shutdown()
	} else {
		providerLabel = "external"
	}

	mode := llm.FailureModeNone
	if *llmTimeout {
		mode = llm.FailureModeTimeout
	}
	cleanupProvider := llm.LLMProvider(llm.NewFakeProvider(llm.FakeConfig{Mode: mode}))
	cleanupLabel := "fake"
	if strings.EqualFold(*llmProvider, "ollama") {
		cleanupProvider = llm.NewOllamaProvider(llm.OllamaConfig{BaseURL: *ollamaBaseURL, Model: *ollamaModel, Timeout: *ollamaTimeout})
		cleanupLabel = "ollama"
	} else if !strings.EqualFold(*llmProvider, "fake") {
		return fmt.Errorf("unsupported --llm-provider %q", *llmProvider)
	}

	injector := inject.TextInjector(inject.NewFakeInjector())
	if *insertionFailure {
		injector = failingInjector{}
	}

	pipeline := app.NewPipeline(
		asr.NewSidecarProvider(asr.Config{URL: selectedASRURL, Version: protocol.VersionV1Alpha1, Timeout: *asrTimeout}),
		cleanupProvider,
		injector,
	)
	if *hostIntegration {
		_, _ = fmt.Fprintf(stdout, "HOST_FULL_SESSION kind=host-integration physical_ios=false asr=%s llm=%s injection=fake\n", providerLabel, cleanupLabel)
	}

	var result app.ProcessResult
	var err error
	if *fullSession {
		result, err = processFullSession(ctx, stdout, pipeline, *fixturePath, *fixtureFormat, *debugWAV)
	} else {
		result, err = pipeline.ProcessPCMFile(ctx, *fixturePath)
	}
	if err != nil {
		if *asrUnavailable {
			_, _ = fmt.Fprintf(stdout, "EXPECTED_ERROR stage=asr err=%q\n", err.Error())
		}
		if *insertionFailure {
			_, _ = fmt.Fprintf(stdout, "EXPECTED_ERROR stage=insertion err=%q\n", err.Error())
			if result.FinalText != "" {
				_, _ = fmt.Fprintf(stdout, "RECOVERABLE_TEXT %s\n", result.FinalText)
			}
		}
		return err
	}

	_, _ = fmt.Fprintf(stdout, "RAW_ASR %s\n", result.RawTranscript)
	_, _ = fmt.Fprintf(stdout, "CLEANUP_STATUS %s\n", result.Cleanup.Status)
	_, _ = fmt.Fprintf(stdout, "TEXT_FINAL %s\n", result.FinalText)
	_, _ = fmt.Fprintf(stdout, "INSERT_OK target=%s status=%s\n", result.Receipt.Target, result.Receipt.Status)
	return nil
}

func processFullSession(ctx context.Context, stdout io.Writer, pipeline *app.Pipeline, fixturePath, fixtureFormat, debugWAVPath string) (app.ProcessResult, error) {
	frames, err := loadFixtureFrames(fixturePath, fixtureFormat)
	if err != nil {
		return app.ProcessResult{}, err
	}
	metadata, decryptedFrames, debugResult, err := decryptFakeAudioSession(frames, debugWAVPath != "")
	if err != nil {
		return app.ProcessResult{}, err
	}
	_, _ = fmt.Fprintf(stdout, "ENCRYPTED_AUDIO_START sample_rate=%d channels=%d encoding=%s frame_ms=%d\n", metadata.SampleRate, metadata.Channels, metadata.Encoding, metadata.FrameDurationMS)
	_, _ = fmt.Fprintf(stdout, "ENCRYPTED_AUDIO_FRAME count=%d\n", len(decryptedFrames))
	_, _ = fmt.Fprintf(stdout, "ENCRYPTED_AUDIO_STOP last_sequence=%d\n", len(decryptedFrames))
	if debugWAVPath != "" {
		if err := os.WriteFile(debugWAVPath, debugResult.WAV, 0o600); err != nil {
			return app.ProcessResult{}, err
		}
		_, _ = fmt.Fprintf(stdout, "DEBUG_WAV_OK path=%s sample_rate=%d channels=%d frames=%d bytes=%d\n", debugWAVPath, debugResult.Metadata.SampleRate, debugResult.Metadata.Channels, debugResult.FrameCount, len(debugResult.WAV))
	}
	return pipeline.ProcessAudioFrames(ctx, metadata, decryptedFrames)
}

func loadFixtureFrames(fixturePath, fixtureFormat string) ([][]byte, error) {
	switch strings.ToLower(strings.TrimSpace(fixtureFormat)) {
	case "", "pcm":
		return asr.LoadPCMFrames(fixturePath, asr.DefaultFrameSize)
	case "wav":
		return asr.LoadWAVFrames(fixturePath, asr.DefaultFrameSize)
	default:
		return nil, fmt.Errorf("unsupported fixture format %q", fixtureFormat)
	}
}

func decryptFakeAudioSession(frames [][]byte, captureDebugWAV bool) (protocol.AudioMetadata, [][]byte, session.DiagnosticAudioCaptureResult, error) {
	client, server, err := linkedStateMachines()
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}

	sessionID := "session-1"
	streamID := "stream-1"
	metadata := asr.DefaultAudioMetadata()
	startPayload, err := protocol.Encode(protocol.AudioStart{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStart}, SessionID: sessionID, StreamID: streamID, Metadata: metadata})
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}
	startMsg, err := client.Encrypt(protocol.MessageTypeAudioStart, startPayload)
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}
	encryptedMessages := []session.EncryptedMessage{startMsg}
	decodedStart, err := decryptProtocolMessage(server, startMsg)
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}
	start, ok := decodedStart.(protocol.AudioStart)
	if !ok {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, fmt.Errorf("decrypted start message = %T", decodedStart)
	}

	decryptedFrames := make([][]byte, 0, len(frames))
	for index, frame := range frames {
		framePayload, err := protocol.Encode(protocol.AudioFrame{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioFrame}, SessionID: sessionID, StreamID: streamID, Sequence: index + 1, PayloadBase64: base64.StdEncoding.EncodeToString(frame)})
		if err != nil {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
		}
		frameMsg, err := client.Encrypt(protocol.MessageTypeAudioFrame, framePayload)
		if err != nil {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
		}
		encryptedMessages = append(encryptedMessages, frameMsg)
		decodedFrame, err := decryptProtocolMessage(server, frameMsg)
		if err != nil {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
		}
		audioFrame, ok := decodedFrame.(protocol.AudioFrame)
		if !ok {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, fmt.Errorf("decrypted frame message = %T", decodedFrame)
		}
		decodedPayload, err := base64.StdEncoding.DecodeString(audioFrame.PayloadBase64)
		if err != nil {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
		}
		decryptedFrames = append(decryptedFrames, decodedPayload)
	}

	stopPayload, err := protocol.Encode(protocol.AudioStop{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStop}, SessionID: sessionID, StreamID: streamID, LastSequence: len(frames)})
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}
	stopMsg, err := client.Encrypt(protocol.MessageTypeAudioStop, stopPayload)
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}
	encryptedMessages = append(encryptedMessages, stopMsg)
	decodedStop, err := decryptProtocolMessage(server, stopMsg)
	if err != nil {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
	}
	stop, ok := decodedStop.(protocol.AudioStop)
	if !ok {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, fmt.Errorf("decrypted stop message = %T", decodedStop)
	}
	if stop.LastSequence != len(frames) {
		return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, fmt.Errorf("last sequence = %d, want %d", stop.LastSequence, len(frames))
	}

	var debugResult session.DiagnosticAudioCaptureResult
	if captureDebugWAV {
		_, debugServer, err := linkedStateMachines()
		if err != nil {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
		}
		capture := session.DiagnosticAudioCapture{Enabled: true}
		var completed bool
		for _, msg := range encryptedMessages {
			debugResult, completed, err = capture.CaptureEncryptedMessage(debugServer, msg)
			if err != nil {
				return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, err
			}
		}
		if !completed {
			return protocol.AudioMetadata{}, nil, session.DiagnosticAudioCaptureResult{}, fmt.Errorf("debug audio capture did not complete")
		}
	}

	return start.Metadata, decryptedFrames, debugResult, nil
}

func decryptProtocolMessage(machine *session.StateMachine, msg session.EncryptedMessage) (any, error) {
	plaintext, err := machine.Decrypt(msg)
	if err != nil {
		return nil, err
	}
	return protocol.Decode(plaintext)
}

func linkedStateMachines() (*session.StateMachine, *session.StateMachine, error) {
	client, err := session.NewStateMachine(session.Config{SessionID: []byte("session-12345678"), SendKey: bytes.Repeat([]byte{1}, 32), ReceiveKey: bytes.Repeat([]byte{2}, 32), InactivityTimeout: 30 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	server, err := session.NewStateMachine(session.Config{SessionID: []byte("session-12345678"), SendKey: bytes.Repeat([]byte{2}, 32), ReceiveKey: bytes.Repeat([]byte{1}, 32), InactivityTimeout: 30 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	return client, server, nil
}

type failingInjector struct{}

func (failingInjector) Insert(context.Context, string) (inject.Receipt, error) {
	return inject.Receipt{}, errors.New("simulated insertion failure")
}

func startFakeRuntime(ctx context.Context, ready bool) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}

	server := &http.Server{Handler: (&asr.FakeRuntime{Ready: ready}).Handler()}
	go func() {
		_ = server.Serve(ln)
	}()

	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = ln.Close()
	}

	return "ws://" + ln.Addr().String() + "/ws", shutdown, nil
}
