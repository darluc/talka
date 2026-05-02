package asr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestProxyRuntimeOffersBinarySubprotocolToFunASRUpstream(t *testing.T) {
	var connections int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !headerContainsToken(req.Header, "Sec-WebSocket-Protocol", "binary") {
			http.Error(w, "missing binary subprotocol", http.StatusBadRequest)
			return
		}

		connectionNumber := atomic.AddInt32(&connections, 1)
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Errorf("acceptWebSocket() error = %v", err)
			return
		}
		defer conn.Close()

		if connectionNumber == 1 {
			return
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Errorf("readFrameWithOpcode(start) error = %v", err)
			return
		} else if opcode != 0x1 {
			t.Errorf("start opcode = %d, want text", opcode)
			return
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Errorf("readFrameWithOpcode(audio) error = %v", err)
			return
		} else if opcode != 0x2 {
			t.Errorf("audio opcode = %d, want binary", opcode)
			return
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Errorf("readFrameWithOpcode(stop) error = %v", err)
			return
		} else if opcode != 0x1 {
			t.Errorf("stop opcode = %d, want text", opcode)
			return
		}

		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Errorf("readFrameWithOpcode(flush) error = %v", err)
			return
		}
		if opcode != 0x2 {
			t.Errorf("flush opcode = %d, want binary", opcode)
			return
		}
		if len(payload) != 0 {
			t.Errorf("flush payload length = %d, want 0", len(payload))
			return
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "你好，世界", "is_final": true}); err != nil {
			t.Errorf("WriteJSON(final) error = %v", err)
		}
	}))
	defer upstream.Close()

	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: websocketURLFromHTTP(t, upstream.URL), Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	result, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := result.TextFinal.Text, "你好，世界"; got != want {
		t.Fatalf("TextFinal.Text = %q, want %q", got, want)
	}
	if got, want := atomic.LoadInt32(&connections), int32(2); got != want {
		t.Fatalf("upstream connections = %d, want %d", got, want)
	}
}

func TestProxyRuntimeTranslatesFunASRResponsesIntoStableSidecarMessages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Fatalf("acceptWebSocket() error = %v", err)
		}
		defer conn.Close()

		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("readFrameWithOpcode(config) error = %v", err)
		}
		if opcode != 0x1 {
			t.Fatalf("opcode = %d, want text", opcode)
		}

		var start funASRStartMessage
		if err := json.Unmarshal(payload, &start); err != nil {
			t.Fatalf("Unmarshal(start) error = %v", err)
		}
		if start.Mode != "2pass" {
			t.Fatalf("Mode = %q, want 2pass", start.Mode)
		}

		if _, opcode, err = conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(audio) error = %v", err)
		}
		if opcode != 0x2 {
			t.Fatalf("audio opcode = %d, want binary", opcode)
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-online", "text": "你好", "is_final": false}); err != nil {
			t.Fatalf("WriteJSON(partial) error = %v", err)
		}
		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "你好，世界", "is_final": true}); err != nil {
			t.Fatalf("WriteJSON(final) error = %v", err)
		}

		payload, opcode, err = conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(stop) error = %v", err)
		}
		if opcode != 0x1 {
			t.Fatalf("stop opcode = %d, want text", opcode)
		}

		var stop map[string]any
		if err := json.Unmarshal(payload, &stop); err != nil {
			t.Fatalf("Unmarshal(stop) error = %v", err)
		}
		if stop["is_speaking"] != false {
			t.Fatalf("is_speaking = %v, want false", stop["is_speaking"])
		}

		payload, opcode, err = conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(flush) error = %v", err)
		}
		if opcode != 0x2 {
			t.Fatalf("flush opcode = %d, want binary", opcode)
		}
		if len(payload) != 0 {
			t.Fatalf("flush payload length = %d, want 0", len(payload))
		}
	}))
	defer upstream.Close()

	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: websocketURLFromHTTP(t, upstream.URL), Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	result, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}

	if got, want := len(result.Partials), 1; got != want {
		t.Fatalf("len(Partials) = %d, want %d", got, want)
	}
	if got, want := result.Partials[0].Text, "你好"; got != want {
		t.Fatalf("Partials[0].Text = %q, want %q", got, want)
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

func TestProxyRuntimeReturnsTypedSidecarUnavailableWhenUpstreamCannotBeReached(t *testing.T) {
	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: "ws://127.0.0.1:1", Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 500 * time.Millisecond})
	_, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err == nil {
		t.Fatal("Transcribe() error = nil, want upstream unavailable error")
	}
	if got, want := protocol.ErrorCodeOf(err), protocol.ErrorCodeSidecarUnavailable; got != want {
		t.Fatalf("ErrorCodeOf() = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("error = %q, want upstream guidance", err)
	}
}

func TestProxyRuntimeReturnsTypedSidecarUnavailableForEmptyUpstreamFinal(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Fatalf("acceptWebSocket() error = %v", err)
		}
		defer conn.Close()

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("readFrameWithOpcode(config) error = %v", err)
		} else if opcode != 0x1 {
			t.Fatalf("config opcode = %d, want text", opcode)
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(audio) error = %v", err)
		} else if opcode != 0x2 {
			t.Fatalf("audio opcode = %d, want binary", opcode)
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(stop) error = %v", err)
		} else if opcode != 0x1 {
			t.Fatalf("stop opcode = %d, want text", opcode)
		}

		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(flush) error = %v", err)
		}
		if opcode != 0x2 {
			t.Fatalf("flush opcode = %d, want binary", opcode)
		}
		if len(payload) != 0 {
			t.Fatalf("flush payload length = %d, want 0", len(payload))
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "", "is_final": true}); err != nil {
			t.Fatalf("WriteJSON(empty final) error = %v", err)
		}
	}))
	defer upstream.Close()

	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: websocketURLFromHTTP(t, upstream.URL), Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	_, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err == nil {
		t.Fatal("Transcribe() error = nil, want typed empty final error")
	}
	if got, want := protocol.ErrorCodeOf(err), protocol.ErrorCodeSidecarUnavailable; got != want {
		t.Fatalf("ErrorCodeOf() = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "empty final transcript") {
		t.Fatalf("error = %q, want empty final transcript guidance", err)
	}
}

func TestProxyRuntimeSendsFlushFrameAfterStopForFunASRFinalization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Fatalf("acceptWebSocket() error = %v", err)
		}
		defer conn.Close()

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("readFrameWithOpcode(config) error = %v", err)
		} else if opcode != 0x1 {
			t.Fatalf("config opcode = %d, want text", opcode)
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(audio) error = %v", err)
		} else if opcode != 0x2 {
			t.Fatalf("audio opcode = %d, want binary", opcode)
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(stop) error = %v", err)
		} else if opcode != 0x1 {
			t.Fatalf("stop opcode = %d, want text", opcode)
		}

		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(flush) error = %v", err)
		}
		if opcode != 0x2 {
			t.Fatalf("flush opcode = %d, want binary", opcode)
		}
		if len(payload) != 0 {
			t.Fatalf("flush payload length = %d, want 0", len(payload))
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "你好，世界", "is_final": true}); err != nil {
			t.Fatalf("WriteJSON(final) error = %v", err)
		}
	}))
	defer upstream.Close()

	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: websocketURLFromHTTP(t, upstream.URL), Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	result, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := result.TextFinal.Text, "你好，世界"; got != want {
		t.Fatalf("TextFinal.Text = %q, want %q", got, want)
	}
}

func TestProxyRuntimeWaitsForDelayedFinalAfterAudioStop(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Fatalf("acceptWebSocket() error = %v", err)
		}
		defer conn.Close()

		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("readFrameWithOpcode(config) error = %v", err)
		}
		if opcode != 0x1 {
			t.Fatalf("opcode = %d, want text", opcode)
		}

		var start funASRStartMessage
		if err := json.Unmarshal(payload, &start); err != nil {
			t.Fatalf("Unmarshal(start) error = %v", err)
		}

		if _, _, err = conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(audio) error = %v", err)
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-online", "text": "你好", "is_final": false}); err != nil {
			t.Fatalf("WriteJSON(partial) error = %v", err)
		}

		payload, opcode, err = conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(stop) error = %v", err)
		}
		if opcode != 0x1 {
			t.Fatalf("stop opcode = %d, want text", opcode)
		}

		payload, opcode, err = conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(flush) error = %v", err)
		}
		if opcode != 0x2 {
			t.Fatalf("flush opcode = %d, want binary", opcode)
		}
		if len(payload) != 0 {
			t.Fatalf("flush payload length = %d, want 0", len(payload))
		}

		time.Sleep(75 * time.Millisecond)
		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "你好，世界", "is_final": true}); err != nil {
			t.Fatalf("WriteJSON(final) error = %v", err)
		}
	}))
	defer upstream.Close()

	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: websocketURLFromHTTP(t, upstream.URL), Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: time.Second})
	result, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := result.TextFinal.Text, "你好，世界"; got != want {
		t.Fatalf("TextFinal.Text = %q, want %q", got, want)
	}
}

func TestProxyRuntimeWaitsLongEnoughForRealFunASRFinalization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Fatalf("acceptWebSocket() error = %v", err)
		}
		defer conn.Close()

		if _, _, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("readFrameWithOpcode(config) error = %v", err)
		}
		if _, _, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(audio) error = %v", err)
		}
		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(stop) error = %v", err)
		} else if opcode != 0x1 {
			t.Fatalf("stop opcode = %d, want text", opcode)
		}
		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			t.Fatalf("readFrameWithOpcode(flush) error = %v", err)
		}
		if opcode != 0x2 {
			t.Fatalf("flush opcode = %d, want binary", opcode)
		}
		if len(payload) != 0 {
			t.Fatalf("flush payload length = %d, want 0", len(payload))
		}

		time.Sleep(1600 * time.Millisecond)
		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "欢迎大家来体验达摩院推出的语音识别模型", "is_final": true}); err != nil {
			t.Fatalf("WriteJSON(final) error = %v", err)
		}
	}))
	defer upstream.Close()

	runtime := NewProxyRuntime(ProxyRuntimeConfig{UpstreamURL: websocketURLFromHTTP(t, upstream.URL), Mode: "2pass"})
	serverURL, shutdown := startRuntimeServer(t, runtime)
	defer shutdown()

	client := NewClient(Config{URL: serverURL, Version: protocol.VersionV1Alpha1, Timeout: 3 * time.Second})
	result, err := client.Transcribe(context.Background(), DefaultAudioMetadata(), [][]byte{make([]byte, 640)})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := result.TextFinal.Text, "欢迎大家来体验达摩院推出的语音识别模型"; got != want {
		t.Fatalf("TextFinal.Text = %q, want %q", got, want)
	}
}

func startRuntimeServer(t *testing.T, runtime http.Handler) (string, func()) {
	t.Helper()
	server := httptest.NewServer(runtime)
	wsURL := websocketURLFromHTTP(t, server.URL) + "/ws"
	return wsURL, server.Close
}

func websocketURLFromHTTP(t *testing.T, raw string) string {
	t.Helper()
	return "ws" + strings.TrimPrefix(raw, "http")
}
