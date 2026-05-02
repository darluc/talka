package app

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"talka/internal/protocol"
	"talka/internal/session"
)

const iosWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	iosWebSocketMaxBufferedMessages = 10000
	iosWebSocketSessionTimeout      = 5 * time.Minute
)

type iosAudioWebSocketResponse struct {
	OK        bool                    `json:"ok"`
	Error     *iosAudioWebSocketError `json:"error,omitempty"`
	FinalText string                  `json:"final_text,omitempty"`
}

type iosAudioWebSocketError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type iosAudioWireMessage struct {
	Version    uint8                `json:"version"`
	SessionID  string               `json:"session_id"`
	Seq        uint64               `json:"seq"`
	Type       protocol.MessageType `json:"type"`
	Nonce      string               `json:"nonce"`
	Ciphertext string               `json:"ciphertext"`
	Tag        string               `json:"tag"`
}

func (a *App) handleIOSAudioStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "audio stream endpoint only accepts WebSocket GET", nil)
		return
	}
	deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "device_id is required", nil)
		return
	}
	conn, reader, err := acceptIOSWebSocket(w, r)
	if err != nil {
		return
	}
	defer conn.Close()
	a.logger.Info("iOS audio websocket accepted", "device_id", deviceID)
	deadline := time.Now().Add(iosWebSocketSessionTimeout)
	_ = conn.SetDeadline(deadline)
	defer conn.SetDeadline(time.Time{})

	var messages []session.EncryptedMessage
	for {
		payload, opcode, err := readIOSWebSocketFrame(reader)
		if err != nil {
			a.logger.Error("iOS audio websocket read failed", "device_id", deviceID, "error", err)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "read"))
			return
		}
		if opcode == 0x8 {
			a.logger.Info("iOS audio websocket closed", "device_id", deviceID, "buffered_messages", len(messages))
			return
		}
		message, err := decodeIOSAudioWireMessage(payload)
		if err != nil {
			a.logger.Error("iOS audio websocket decode failed", "device_id", deviceID, "error", err)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "decode"))
			return
		}
		if !iosWebSocketCanBuffer(len(messages) + 1) {
			a.logger.Error("iOS audio websocket message limit exceeded", "device_id", deviceID, "buffered_messages", len(messages), "limit", iosWebSocketMaxBufferedMessages)
			_ = writeIOSWebSocketError(conn, iosAudioWebSocketError{Code: "message_limit_exceeded", Message: "audio session message limit exceeded"})
			return
		}
		messages = append(messages, message)
		if shouldLogIOSAudioMessage(message, len(messages)) {
			a.logger.Info("iOS audio websocket message received", "device_id", deviceID, "type", message.Type, "seq", message.Seq, "buffered_messages", len(messages))
		}
		if message.Type != protocol.MessageTypeAudioStop {
			continue
		}
		a.logger.Info("iOS audio websocket stop received", "device_id", deviceID, "buffered_messages", len(messages))
		ctx, cancel := context.WithTimeout(r.Context(), time.Until(deadline))
		result, err := a.ProcessEncryptedIOSAudioSession(ctx, deviceID, messages)
		cancel()
		if err != nil {
			a.logger.Error("iOS audio session processing failed", "device_id", deviceID, "buffered_messages", len(messages), "error", err)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
			return
		}
		a.logger.Info("iOS audio session processed", "device_id", deviceID, "buffered_messages", len(messages), "raw_transcript_chars", len(result.RawTranscript), "final_text_chars", len(result.FinalText))
		_ = writeIOSWebSocketJSON(conn, iosAudioWebSocketResponse{OK: true, FinalText: result.FinalText})
		return
	}
}

func shouldLogIOSAudioMessage(message session.EncryptedMessage, bufferedMessages int) bool {
	if message.Type != protocol.MessageTypeAudioFrame {
		return true
	}
	return bufferedMessages == 2 || message.Seq%50 == 0
}

func iosWebSocketCanBuffer(messageCount int) bool {
	return messageCount <= iosWebSocketMaxBufferedMessages
}

func iosWebSocketErrorCode(err error, phase string) iosAudioWebSocketError {
	if errors.Is(err, session.ErrReplay) {
		return iosAudioWebSocketError{Code: "replayed_sequence", Message: "audio session message was already processed"}
	}
	if errors.Is(err, session.ErrOutOfOrder) {
		return iosAudioWebSocketError{Code: "out_of_order_sequence", Message: "audio session messages arrived out of order"}
	}
	if errors.Is(err, session.ErrExpired) || errors.Is(err, context.DeadlineExceeded) {
		return iosAudioWebSocketError{Code: "session_expired", Message: "audio session expired"}
	}
	if errors.Is(err, session.ErrForgotten) {
		return iosAudioWebSocketError{Code: "session_forbidden", Message: "audio session is no longer trusted"}
	}
	switch phase {
	case "read":
		return iosAudioWebSocketError{Code: "read_failed", Message: "websocket frame could not be read"}
	case "decode":
		return iosAudioWebSocketError{Code: "invalid_message", Message: "audio session message is invalid"}
	default:
		return iosAudioWebSocketError{Code: "processing_failed", Message: "audio session could not be processed"}
	}
}

func decodeIOSAudioWireMessage(payload []byte) (session.EncryptedMessage, error) {
	var wire iosAudioWireMessage
	if err := json.Unmarshal(payload, &wire); err != nil {
		return session.EncryptedMessage{}, err
	}
	sessionID, err := base64.StdEncoding.DecodeString(wire.SessionID)
	if err != nil {
		return session.EncryptedMessage{}, fmt.Errorf("decode session_id: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(wire.Nonce)
	if err != nil {
		return session.EncryptedMessage{}, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(wire.Ciphertext)
	if err != nil {
		return session.EncryptedMessage{}, fmt.Errorf("decode ciphertext: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(wire.Tag)
	if err != nil {
		return session.EncryptedMessage{}, fmt.Errorf("decode tag: %w", err)
	}
	return session.EncryptedMessage{Version: wire.Version, SessionID: sessionID, Seq: wire.Seq, Type: wire.Type, Nonce: nonce, Ciphertext: ciphertext, Tag: tag}, nil
}

func acceptIOSWebSocket(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader, error) {
	if !headerContainsToken(r.Header, "Connection", "Upgrade") || !headerContainsToken(r.Header, "Upgrade", "websocket") {
		http.Error(w, "upgrade required", http.StatusUpgradeRequired)
		return nil, nil, fmt.Errorf("request is not a websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return nil, nil, fmt.Errorf("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket hijack unsupported", http.StatusInternalServerError)
		return nil, nil, fmt.Errorf("websocket hijack unsupported")
	}
	conn, readerWriter, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	acceptHash := sha1.Sum([]byte(key + iosWebSocketGUID))
	accept := base64.StdEncoding.EncodeToString(acceptHash[:])
	response := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if _, err := conn.Write([]byte(response)); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, readerWriter.Reader, nil
}

func readIOSWebSocketFrame(reader *bufio.Reader) ([]byte, byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, 0, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	if length == 126 {
		var extended [2]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return nil, 0, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
	} else if length == 127 {
		var extended [8]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return nil, 0, err
		}
		length = binary.BigEndian.Uint64(extended[:])
	}
	if length > 1<<20 {
		return nil, 0, fmt.Errorf("websocket payload too large: %d", length)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(reader, mask[:]); err != nil {
			return nil, 0, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, 0, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%4]
		}
	}
	return payload, opcode, nil
}

func writeIOSWebSocketText(conn net.Conn, text string) error {
	payload := []byte(text)
	frame := []byte{0x81}
	if len(payload) < 126 {
		frame = append(frame, byte(len(payload)))
	} else if len(payload) <= 65535 {
		frame = append(frame, 126, byte(len(payload)>>8), byte(len(payload)))
	} else {
		return fmt.Errorf("websocket response too large")
	}
	frame = append(frame, payload...)
	_, err := conn.Write(frame)
	return err
}

func writeIOSWebSocketJSON(conn net.Conn, response iosAudioWebSocketResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return writeIOSWebSocketText(conn, string(payload))
}

func writeIOSWebSocketError(conn net.Conn, safe iosAudioWebSocketError) error {
	return writeIOSWebSocketJSON(conn, iosAudioWebSocketResponse{OK: false, Error: &safe})
}

func headerContainsToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
