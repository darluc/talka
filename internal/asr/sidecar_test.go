package asr

import (
	"context"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestClientTranscribeReturnsDeterministicTranscript(t *testing.T) {
	runtime := &FakeRuntime{Ready: true}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	metadata := protocol.AudioMetadata{SampleRate: 16000, Channels: 1, Encoding: protocol.EncodingPCMS16LE, FrameDurationMS: 20, Language: "zh-CN"}
	frames := [][]byte{make([]byte, 640), make([]byte, 640)}

	result, err := client.Transcribe(context.Background(), metadata, frames)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}

	if len(result.Partials) != 2 {
		t.Fatalf("len(Partials) = %d, want 2", len(result.Partials))
	}

	if got, want := result.Partials[0].Text, "你好"; got != want {
		t.Fatalf("Partials[0].Text = %q, want %q", got, want)
	}

	if got, want := result.Partials[1].Text, "你好，世界"; got != want {
		t.Fatalf("Partials[1].Text = %q, want %q", got, want)
	}

	if got, want := len(result.Finals), 1; got != want {
		t.Fatalf("len(Finals) = %d, want %d", got, want)
	}

	if got, want := result.Finals[0].Text, "你好，世界"; got != want {
		t.Fatalf("Finals[0].Text = %q, want %q", got, want)
	}

	if got, want := result.TextFinal.Text, "你好，世界"; got != want {
		t.Fatalf("TextFinal.Text = %q, want %q", got, want)
	}
}

func TestClientStreamFeedsFramesIncrementally(t *testing.T) {
	runtime := &FakeRuntime{Ready: true}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	stream, err := client.NewStream(context.Background(), DefaultAudioMetadata())
	if err != nil {
		t.Fatalf("NewStream() error = %v", err)
	}
	defer stream.Close(context.Background())

	first, err := stream.AcceptFrame(context.Background(), 0, make([]byte, DefaultFrameSize))
	if err != nil {
		t.Fatalf("AcceptFrame(first) error = %v", err)
	}
	if got, want := len(first.Partials), 1; got != want {
		t.Fatalf("len(first.Partials) = %d, want %d", got, want)
	}
	if got, want := first.Partials[0].Text, "你好"; got != want {
		t.Fatalf("first partial = %q, want %q", got, want)
	}

	second, err := stream.AcceptFrame(context.Background(), 0, make([]byte, DefaultFrameSize))
	if err != nil {
		t.Fatalf("AcceptFrame(second) error = %v", err)
	}
	if got, want := len(second.Partials), 2; got != want {
		t.Fatalf("len(second.Partials) = %d, want %d", got, want)
	}

	final, err := stream.Finish(context.Background())
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if got, want := final.TextFinal.Text, "你好，世界"; got != want {
		t.Fatalf("TextFinal.Text = %q, want %q", got, want)
	}
}

func TestClientTranscribeRejectsUnsupportedEncoding(t *testing.T) {
	runtime := &FakeRuntime{Ready: true}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	metadata := protocol.AudioMetadata{SampleRate: 16000, Channels: 1, Encoding: "opus", FrameDurationMS: 20, Language: "zh-CN"}

	_, err := client.Transcribe(context.Background(), metadata, [][]byte{make([]byte, 640)})
	if err == nil {
		t.Fatal("Transcribe() error = nil, want unsupported encoding error")
	}

	if got, want := protocol.ErrorCodeOf(err), protocol.ErrorCodeUnsupportedEncoding; got != want {
		t.Fatalf("ErrorCodeOf() = %q, want %q", got, want)
	}
}

func TestRuntimeRejectsOutOfOrderFrames(t *testing.T) {
	runtime := &FakeRuntime{Ready: true}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	conn, err := dialWebSocket(context.Background(), serverURL, 2*time.Second)
	if err != nil {
		t.Fatalf("dialWebSocket() error = %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(protocol.ClientHello{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeClientHello}, ClientName: "test-client", SessionID: "session-1"}); err != nil {
		t.Fatalf("WriteJSON(client_hello) error = %v", err)
	}

	if _, err := readMessage(context.Background(), conn); err != nil {
		t.Fatalf("readMessage(server_hello) error = %v", err)
	}

	if err := conn.WriteJSON(protocol.AudioStart{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStart}, SessionID: "session-1", StreamID: "stream-1", Metadata: protocol.AudioMetadata{SampleRate: 16000, Channels: 1, Encoding: protocol.EncodingPCMS16LE, FrameDurationMS: 20, Language: "zh-CN"}}); err != nil {
		t.Fatalf("WriteJSON(audio_start) error = %v", err)
	}

	if err := conn.WriteJSON(protocol.AudioFrame{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioFrame}, SessionID: "session-1", StreamID: "stream-1", Sequence: 2, PayloadBase64: "AAECAw=="}); err != nil {
		t.Fatalf("WriteJSON(audio_frame) error = %v", err)
	}

	msg, err := readMessage(context.Background(), conn)
	if err != nil {
		t.Fatalf("readMessage(error) error = %v", err)
	}

	errMsg, ok := msg.(protocol.ErrorMessage)
	if !ok {
		t.Fatalf("message type = %T, want protocol.ErrorMessage", msg)
	}

	if got, want := errMsg.Code, protocol.ErrorCodeOutOfOrderAudioFrame; got != want {
		t.Fatalf("Error code = %q, want %q", got, want)
	}
}

func TestClientHealthCheckFailsWhenRuntimeIsUnhealthy(t *testing.T) {
	runtime := &FakeRuntime{Ready: false}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})

	err := client.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck() error = nil, want sidecar unavailable error")
	}

	if got, want := protocol.ErrorCodeOf(err), protocol.ErrorCodeSidecarUnavailable; got != want {
		t.Fatalf("ErrorCodeOf() = %q, want %q", got, want)
	}
}
