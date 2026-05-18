package asr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestManagedSidecarProviderStartsRuntimeAndUsesSidecarProtocol(t *testing.T) {
	runtime := &FakeRuntime{Ready: true}
	serverURL, shutdown := startFakeRuntimeServer(t, runtime)
	defer shutdown()

	manager := &sidecarManagerProbe{url: serverURL, ready: make(chan struct{}, 1)}
	provider := NewManagedSidecarProvider(manager, Config{Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})

	result, err := provider.Transcribe(context.Background(), Request{
		Metadata: DefaultAudioMetadata(),
		Frames:   [][]byte{make([]byte, DefaultFrameSize), make([]byte, DefaultFrameSize)},
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := result.Transcript, "你好，世界"; got != want {
		t.Fatalf("Transcript = %q, want %q", got, want)
	}

	select {
	case <-manager.ready:
	default:
		t.Fatal("manager EnsureRunning was not called before sidecar transcription")
	}
}

func websocketURLFromHTTP(t *testing.T, raw string) string {
	t.Helper()
	return "ws" + strings.TrimPrefix(raw, "http")
}

func TestManagedSidecarProviderEnsureReadyRequiresSidecarProtocol(t *testing.T) {
	handshakeOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Errorf("acceptWebSocket() error = %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer handshakeOnly.Close()

	manager := &sidecarManagerProbe{url: websocketURLFromHTTP(t, handshakeOnly.URL), ready: make(chan struct{}, 1)}
	provider := NewManagedSidecarProvider(manager, Config{Version: protocol.VersionV1Alpha1, Timeout: 200 * time.Millisecond})

	if err := provider.EnsureReady(context.Background()); err == nil {
		t.Fatal("EnsureReady() error = nil, want protocol health failure")
	}
}

type sidecarManagerProbe struct {
	url   string
	ready chan struct{}
}

func (m *sidecarManagerProbe) EnsureRunning(context.Context) error {
	select {
	case m.ready <- struct{}{}:
	default:
	}
	return nil
}

func (m *sidecarManagerProbe) URL() string {
	return m.url
}
