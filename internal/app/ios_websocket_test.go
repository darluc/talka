package app

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"talka/internal/asr"
)

func TestIOSAudioWebSocketReturnsSanitizedDecodeError(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()

	conn, reader := dialIOSAudioWebSocket(t, server.URL, "iphone-1")
	defer conn.Close()

	writeClientWebSocketText(t, conn, `{`)
	payload, opcode, err := readIOSWebSocketFrame(reader)
	if err != nil {
		t.Fatalf("readIOSWebSocketFrame() error = %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("opcode = %#x, want text", opcode)
	}

	var response struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", string(payload), err)
	}
	if response.OK {
		t.Fatal("OK = true, want false")
	}
	if response.Error.Code != "invalid_message" {
		t.Fatalf("Error.Code = %q, want invalid_message", response.Error.Code)
	}
	if strings.Contains(response.Error.Message, "invalid character") || strings.Contains(string(payload), "invalid character") {
		t.Fatalf("websocket response leaked raw decode error: %s", string(payload))
	}
}

func TestIOSWebSocketErrorCodeReturnsSpecificASRFailure(t *testing.T) {
	err := asr.NewRuntimeErrorWithDiagnostic(
		asr.ErrorCodeRuntimeUnavailable,
		"embedded FunASR runtime startup failed",
		"missing language model bundle: /Applications/TalkaMac.app/Contents/Resources/models/funasr/speech_ngram_lm_zh-cn-ai-wesp-fst/TLG.fst",
		errors.New("embedded runtime aborted"),
	)

	response := iosWebSocketErrorCode(err, "process")

	if response.Code != "asr_runtime_unavailable" {
		t.Fatalf("Code = %q, want asr_runtime_unavailable", response.Code)
	}
	if response.Message == "audio session could not be processed" {
		t.Fatalf("Message = %q, want specific ASR guidance", response.Message)
	}
	if !strings.Contains(response.Message, "ASR") && !strings.Contains(response.Message, "language model") {
		t.Fatalf("Message = %q, want ASR-specific context", response.Message)
	}
}

func TestIOSWebSocketErrorCodeReturnsSpecificASRStartupTimeoutFailure(t *testing.T) {
	err := asr.NewRuntimeErrorWithDiagnostic(
		asr.ErrorCodeRuntimeStartupFailed,
		"embedded FunASR runtime could not start",
		"runtime did not become healthy within 5s",
		errors.New("context deadline exceeded"),
	)

	response := iosWebSocketErrorCode(err, "process")

	if response.Code != "asr_runtime_startup_failed" {
		t.Fatalf("Code = %q, want asr_runtime_startup_failed", response.Code)
	}
	if got, want := response.Message, "The Mac ASR runtime took too long to become ready."; got != want {
		t.Fatalf("Message = %q, want %q", got, want)
	}
}

func TestIOSAudioWebSocketBufferLimitRejectsNextMessage(t *testing.T) {
	messages := make([]sessionMessageStub, iosWebSocketMaxBufferedMessages)
	if !iosWebSocketCanBuffer(len(messages)) {
		t.Fatal("full buffer should still represent the maximum accepted messages")
	}
	if iosWebSocketCanBuffer(len(messages) + 1) {
		t.Fatal("message beyond maximum buffer should be rejected")
	}
}

type sessionMessageStub struct{}

func dialIOSAudioWebSocket(t *testing.T, baseURL, deviceID string) (net.Conn, *bufio.Reader) {
	t.Helper()
	addr := strings.TrimPrefix(baseURL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial(%q) error = %v", addr, err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	request := "GET /v1/session/audio?device_id=" + deviceID + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		conn.Close()
		t.Fatalf("Write(handshake) error = %v", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		t.Fatalf("ReadResponse(handshake) error = %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		t.Fatalf("StatusCode = %d, want 101", resp.StatusCode)
	}
	return conn, reader
}

func writeClientWebSocketText(t *testing.T, conn net.Conn, text string) {
	t.Helper()
	payload := []byte(text)
	if len(payload) >= 126 {
		t.Fatalf("test helper only supports small payloads, got %d", len(payload))
	}
	mask := [4]byte{1, 2, 3, 4}
	frame := []byte{0x81, 0x80 | byte(len(payload)), mask[0], mask[1], mask[2], mask[3]}
	for index, b := range payload {
		frame = append(frame, b^mask[index%len(mask)])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("Write(websocket frame) error = %v", err)
	}
}

var _ = httptest.NewServer
