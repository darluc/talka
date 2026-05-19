package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"talka/internal/asr"
	"talka/internal/inject"
	"talka/internal/protocol"
	"talka/internal/session"
)

const iosWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	iosWebSocketMaxBufferedMessages = 10000
	iosWebSocketSessionTimeout      = 5 * time.Minute
	iosInsertTimeout                = 10 * time.Second
)

type iosAudioWebSocketResponse struct {
	OK        bool                    `json:"ok"`
	Error     *iosAudioWebSocketError `json:"error,omitempty"`
	FinalText string                  `json:"final_text,omitempty"`
}

type iosAudioWebSocketError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Diagnostic string `json:"diagnostic,omitempty"`
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
	a.registerIOSAudioConn(conn)
	defer a.unregisterIOSAudioConn(conn)
	defer conn.Close()
	traceID := newLatencyTraceID()
	acceptedAt := time.Now()
	a.latencyRecorder.Start(traceID, deviceID, acceptedAt)
	a.logger.Info("iOS audio websocket accepted", "device_id", deviceID)
	deadline := time.Now().Add(iosWebSocketSessionTimeout)
	_ = conn.SetDeadline(deadline)
	defer conn.SetDeadline(time.Time{})

	firstMessage, ok := a.readInitialIOSAudioWireMessage(conn, reader, deviceID, traceID)
	if !ok {
		return
	}
	if firstMessage.Type == protocol.MessageTypeKeyPress {
		if err := a.handleIOSKeyPressWireMessage(r.Context(), conn, deviceID, traceID, deadline, firstMessage); err != nil {
			a.logger.Error("iOS key press failed", "device_id", deviceID, "error", err)
		}
		return
	}

	if handled, err := a.handleIOSAudioStreamWithStreamingASR(r.Context(), conn, reader, deviceID, traceID, deadline, &firstMessage); handled {
		if err != nil {
			a.logger.Error("iOS streaming audio session failed", "device_id", deviceID, "error", err)
		}
		return
	}

	messages := []session.EncryptedMessage{firstMessage}
	for {
		if messages[len(messages)-1].Type == protocol.MessageTypeAudioStop {
			stopReceivedAt := time.Now()
			a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
				trace.AudioStopReceivedAt = stopReceivedAt
				trace.BufferedMessages = len(messages)
			})
			a.logger.Info("iOS audio websocket stop received", "device_id", deviceID, "buffered_messages", len(messages))
			ctx, cancel := context.WithTimeout(r.Context(), time.Until(deadline))
			result, err := a.processEncryptedIOSAudioSession(ctx, deviceID, traceID, messages)
			cancel()
			if err != nil {
				a.logger.Error("iOS audio session processing failed", "device_id", deviceID, "buffered_messages", len(messages), "error", err)
				a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
					trace.CompletedAt = time.Now()
					trace.TotalAfterStopMS = durationMilliseconds(trace.CompletedAt.Sub(stopReceivedAt))
				})
				if writeErr := writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process")); writeErr != nil {
					a.logger.Error("iOS audio websocket error response write failed", "device_id", deviceID, "error", writeErr)
				} else if closeErr := writeIOSWebSocketClose(conn, 1000, ""); closeErr != nil {
					a.logger.Error("iOS audio websocket close write failed", "device_id", deviceID, "error", closeErr)
				}
				return
			}
			processedAt := time.Now()
			a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
				trace.CompletedAt = processedAt
				trace.TotalAfterStopMS = durationMilliseconds(processedAt.Sub(stopReceivedAt))
			})
			a.logger.Info("iOS audio session processed", "trace_id", traceID, "device_id", deviceID, "buffered_messages", len(messages), "raw_transcript_chars", len(result.RawTranscript), "final_text_chars", len(result.FinalText))
			responseWriteStartedAt := time.Now()
			if err := writeIOSWebSocketJSON(conn, iosAudioWebSocketResponse{OK: true, FinalText: result.FinalText}); err != nil {
				a.recordLatencyError(traceID, "response_write", err)
				a.logger.Error("iOS audio websocket response write failed", "device_id", deviceID, "error", err)
			} else if closeErr := writeIOSWebSocketClose(conn, 1000, ""); closeErr != nil {
				a.recordLatencyError(traceID, "response_close", closeErr)
				a.logger.Error("iOS audio websocket close write failed", "device_id", deviceID, "error", closeErr)
			}
			a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
				trace.ResponseWriteMS = durationMilliseconds(time.Since(responseWriteStartedAt))
			})
			a.queueIOSFinalTextInsertion(traceID, deviceID, result.FinalText)
			return
		}

		payload, opcode, err := readIOSWebSocketFrame(reader)
		if err != nil {
			a.recordLatencyError(traceID, "read", err)
			a.completeLatencyTrace(traceID)
			a.logger.Error("iOS audio websocket read failed", "device_id", deviceID, "error", err)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "read"))
			return
		}
		if opcode == 0x8 {
			a.completeLatencyTrace(traceID)
			a.logger.Info("iOS audio websocket closed", "device_id", deviceID, "buffered_messages", len(messages))
			return
		}
		message, err := decodeIOSAudioWireMessage(payload)
		if err != nil {
			a.recordLatencyError(traceID, "decode", err)
			a.completeLatencyTrace(traceID)
			a.logger.Error("iOS audio websocket decode failed", "device_id", deviceID, "error", err)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "decode"))
			return
		}
		if message.Type == protocol.MessageTypeAudioStart || message.Type == protocol.MessageTypeAudioStop {
			a.logger.Info("iOS audio websocket control message", "device_id", deviceID, "type", message.Type, "seq", message.Seq)
		}
		if !iosWebSocketCanBuffer(len(messages) + 1) {
			a.recordLatencyError(traceID, "buffer", fmt.Errorf("message_limit_exceeded"))
			a.completeLatencyTrace(traceID)
			a.logger.Error("iOS audio websocket message limit exceeded", "device_id", deviceID, "buffered_messages", len(messages), "limit", iosWebSocketMaxBufferedMessages)
			_ = writeIOSWebSocketError(conn, iosAudioWebSocketError{Code: "message_limit_exceeded", Message: "audio session message limit exceeded"})
			return
		}
		messages = append(messages, message)
		if shouldLogIOSAudioMessage(message, len(messages)) {
			a.logger.Info("iOS audio websocket message received", "device_id", deviceID, "type", message.Type, "seq", message.Seq, "buffered_messages", len(messages))
		}
	}
}

func (a *App) readInitialIOSAudioWireMessage(conn net.Conn, reader *bufio.Reader, deviceID, traceID string) (session.EncryptedMessage, bool) {
	payload, opcode, err := readIOSWebSocketFrame(reader)
	if err != nil {
		a.recordLatencyError(traceID, "read", err)
		a.completeLatencyTrace(traceID)
		a.logger.Error("iOS audio websocket read failed", "device_id", deviceID, "error", err)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "read"))
		return session.EncryptedMessage{}, false
	}
	if opcode == 0x8 {
		a.completeLatencyTrace(traceID)
		a.logger.Info("iOS audio websocket closed", "device_id", deviceID)
		return session.EncryptedMessage{}, false
	}
	message, err := decodeIOSAudioWireMessage(payload)
	if err != nil {
		a.recordLatencyError(traceID, "decode", err)
		a.completeLatencyTrace(traceID)
		a.logger.Error("iOS audio websocket decode failed", "device_id", deviceID, "error", err)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "decode"))
		return session.EncryptedMessage{}, false
	}
	if message.Type == protocol.MessageTypeAudioStart || message.Type == protocol.MessageTypeAudioStop || message.Type == protocol.MessageTypeKeyPress {
		a.logger.Info("iOS audio websocket control message", "device_id", deviceID, "type", message.Type, "seq", message.Seq)
	}
	return message, true
}

func (a *App) handleIOSAudioStreamWithStreamingASR(parentCtx context.Context, conn net.Conn, reader *bufio.Reader, deviceID, traceID string, deadline time.Time, initialMessage *session.EncryptedMessage) (bool, error) {
	machine, pipeline, streamingProvider, releasePipeline, ok, err := a.iosStreamingAudioResources(deviceID)
	if err != nil {
		a.recordLatencyError(traceID, "session", err)
		a.completeLatencyTrace(traceID)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "session"))
		return true, err
	}
	if !ok {
		return false, nil
	}
	defer releasePipeline()

	ctx, cancel := context.WithTimeout(parentCtx, time.Until(deadline))
	defer cancel()

	var (
		stream         asr.StreamingSession
		streamID       string
		metadata       protocol.AudioMetadata
		started        bool
		frames         int
		messages       int
		asrProcessing  time.Duration
		stopReceivedAt time.Time
	)
	defer func() {
		if stream != nil {
			_ = stream.Close(context.Background())
		}
	}()

	pendingMessage := initialMessage
	for {
		var message session.EncryptedMessage
		if pendingMessage != nil {
			message = *pendingMessage
			pendingMessage = nil
		} else {
			payload, opcode, err := readIOSWebSocketFrame(reader)
			if err != nil {
				a.recordLatencyError(traceID, "read", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "read"))
				return true, err
			}
			if opcode == 0x8 {
				a.completeLatencyTrace(traceID)
				a.logger.Info("iOS audio websocket closed", "device_id", deviceID, "buffered_messages", messages)
				return true, nil
			}
			message, err = decodeIOSAudioWireMessage(payload)
			if err != nil {
				a.recordLatencyError(traceID, "decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "decode"))
				return true, err
			}
		}
		messages++
		if shouldLogIOSAudioMessage(message, messages) {
			a.logger.Info("iOS streaming audio websocket message received", "device_id", deviceID, "type", message.Type, "seq", message.Seq, "messages", messages)
		}
		decoded, err := decryptIOSAudioMessage(machine, message)
		if err != nil {
			a.recordLatencyError(traceID, "decrypt_decode", err)
			a.completeLatencyTrace(traceID)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
			return true, err
		}

		switch typed := decoded.(type) {
		case protocol.KeyPress:
			if err := a.handleIOSKeyPress(ctx, pipeline, typed); err != nil {
				a.recordLatencyError(traceID, "key_press", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			if err := writeIOSWebSocketJSON(conn, iosAudioWebSocketResponse{OK: true}); err != nil {
				a.recordLatencyError(traceID, "response_write", err)
				return true, err
			}
			if err := writeIOSWebSocketClose(conn, 1000, ""); err != nil {
				a.recordLatencyError(traceID, "response_close", err)
				return true, err
			}
			a.completeLatencyTrace(traceID)
			return true, nil
		case protocol.AudioStart:
			if started {
				err := fmt.Errorf("duplicate audio_start")
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			metadata = typed.Metadata
			streamID = typed.StreamID
			stream, err = streamingProvider.NewStream(ctx, metadata)
			if err != nil {
				a.recordLatencyError(traceID, "pipeline", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			started = true
			a.logger.Info("iOS streaming audio started", "trace_id", traceID, "device_id", deviceID, "sample_rate", metadata.SampleRate, "channels", metadata.Channels, "encoding", metadata.Encoding, "frame_duration_ms", metadata.FrameDurationMS)
		case protocol.AudioFrame:
			if !started || stream == nil {
				err := fmt.Errorf("audio_start is required before audio_frame")
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			if typed.StreamID != streamID {
				err := fmt.Errorf("audio frame stream id mismatch")
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			if typed.Sequence != frames+1 {
				err := fmt.Errorf("audio frame sequence = %d, want %d", typed.Sequence, frames+1)
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			frame, err := base64.StdEncoding.DecodeString(typed.PayloadBase64)
			if err != nil {
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			frames++
			startedAt := time.Now()
			if _, err := stream.AcceptFrame(ctx, frame); err != nil {
				a.recordLatencyError(traceID, "pipeline", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			asrProcessing += time.Since(startedAt)
		case protocol.AudioStop:
			if !started || stream == nil {
				err := fmt.Errorf("audio_start is required before audio_stop")
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			if typed.StreamID != streamID {
				err := fmt.Errorf("audio stop stream id mismatch")
				a.recordLatencyError(traceID, "decrypt_decode", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			stopReceivedAt = time.Now()
			a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
				trace.AudioStopReceivedAt = stopReceivedAt
				trace.BufferedMessages = messages
				trace.Frames = frames
				trace.AudioMSEstimate = int64(frames * metadata.FrameDurationMS)
			})
			startedAt := time.Now()
			asrResult, err := stream.Finish(ctx)
			asrProcessing += time.Since(startedAt)
			if err != nil {
				a.recordLatencyError(traceID, "pipeline", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			result, llmDuration, err := pipeline.PrepareASRResult(ctx, asrResult)
			if err != nil {
				a.recordLatencyError(traceID, "pipeline", err)
				a.completeLatencyTrace(traceID)
				_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
				return true, err
			}
			processedAt := time.Now()
			a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
				trace.ASRMS = durationMilliseconds(asrProcessing)
				trace.LLMMS = durationMilliseconds(llmDuration)
				trace.RawTranscriptChars = len(result.RawTranscript)
				trace.FinalTextChars = len(result.FinalText)
				trace.CompletedAt = processedAt
				trace.TotalAfterStopMS = durationMilliseconds(processedAt.Sub(stopReceivedAt))
			})
			responseWriteStartedAt := time.Now()
			if err := writeIOSWebSocketJSON(conn, iosAudioWebSocketResponse{OK: true, FinalText: result.FinalText}); err != nil {
				a.recordLatencyError(traceID, "response_write", err)
				return true, err
			}
			if err := writeIOSWebSocketClose(conn, 1000, ""); err != nil {
				a.recordLatencyError(traceID, "response_close", err)
				return true, err
			}
			a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
				trace.ResponseWriteMS = durationMilliseconds(time.Since(responseWriteStartedAt))
			})
			a.queueIOSFinalTextInsertion(traceID, deviceID, result.FinalText)
			return true, nil
		default:
			err := fmt.Errorf("unexpected message type %T", decoded)
			a.recordLatencyError(traceID, "decrypt_decode", err)
			a.completeLatencyTrace(traceID)
			_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
			return true, err
		}
	}
}

func (a *App) iosStreamingAudioResources(deviceID string) (*session.StateMachine, *Pipeline, asr.StreamingProvider, func(), bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	active := a.iosSessions[deviceID]
	if active == nil {
		return nil, nil, nil, nil, false, nil
	}
	if a.pipeline == nil {
		return nil, nil, nil, nil, false, nil
	}
	streamingProvider, ok := a.pipeline.asr.(asr.StreamingProvider)
	if !ok {
		return active.machine, a.pipeline, nil, nil, false, nil
	}
	release := a.retainPipelineLocked(a.pipeline)
	return active.machine, a.pipeline, streamingProvider, release, true, nil
}

func (a *App) iosKeyPressResources(deviceID string) (*session.StateMachine, *Pipeline, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	active := a.iosSessions[deviceID]
	if active == nil {
		return nil, nil, nil, fmt.Errorf("paired iOS session is not active")
	}
	if a.pipeline == nil {
		return nil, nil, nil, fmt.Errorf("audio pipeline is not configured")
	}
	release := a.retainPipelineLocked(a.pipeline)
	return active.machine, a.pipeline, release, nil
}

func (a *App) handleIOSKeyPressWireMessage(parentCtx context.Context, conn net.Conn, deviceID, traceID string, deadline time.Time, message session.EncryptedMessage) error {
	machine, pipeline, releasePipeline, err := a.iosKeyPressResources(deviceID)
	if err != nil {
		a.recordLatencyError(traceID, "session", err)
		a.completeLatencyTrace(traceID)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "session"))
		return err
	}
	defer releasePipeline()

	ctx, cancel := context.WithTimeout(parentCtx, time.Until(deadline))
	defer cancel()

	decoded, err := decryptIOSAudioMessage(machine, message)
	if err != nil {
		a.recordLatencyError(traceID, "decrypt_decode", err)
		a.completeLatencyTrace(traceID)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
		return err
	}
	typed, ok := decoded.(protocol.KeyPress)
	if !ok {
		err := fmt.Errorf("unexpected key press payload type %T", decoded)
		a.recordLatencyError(traceID, "decrypt_decode", err)
		a.completeLatencyTrace(traceID)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
		return err
	}
	if err := a.handleIOSKeyPress(ctx, pipeline, typed); err != nil {
		a.recordLatencyError(traceID, "key_press", err)
		a.completeLatencyTrace(traceID)
		_ = writeIOSWebSocketError(conn, iosWebSocketErrorCode(err, "process"))
		return err
	}
	if err := writeIOSWebSocketJSON(conn, iosAudioWebSocketResponse{OK: true}); err != nil {
		a.recordLatencyError(traceID, "response_write", err)
		return err
	}
	if err := writeIOSWebSocketClose(conn, 1000, ""); err != nil {
		a.recordLatencyError(traceID, "response_close", err)
		return err
	}
	a.completeLatencyTrace(traceID)
	return nil
}

func (a *App) handleIOSKeyPress(ctx context.Context, pipeline *Pipeline, message protocol.KeyPress) error {
	if message.Key != "enter" {
		return fmt.Errorf("unsupported key press %q", message.Key)
	}
	modifiers := make([]inject.KeyModifier, 0, len(message.Modifiers))
	for _, modifier := range message.Modifiers {
		switch modifier {
		case protocol.KeyModifierCommand:
			modifiers = append(modifiers, inject.KeyModifierCommand)
		case protocol.KeyModifierAlt:
			modifiers = append(modifiers, inject.KeyModifierAlt)
		case protocol.KeyModifierShift:
			modifiers = append(modifiers, inject.KeyModifierShift)
		default:
			return fmt.Errorf("unsupported key modifier %q", modifier)
		}
	}
	return pipeline.PressKey(ctx, inject.KeyPressRequest{
		Key:       inject.KeyEnter,
		Modifiers: modifiers,
	})
}

func (a *App) queueIOSFinalTextInsertion(traceID, deviceID, finalText string) {
	if strings.TrimSpace(finalText) == "" {
		return
	}

	a.mu.RLock()
	pipeline := a.pipeline
	a.mu.RUnlock()
	if pipeline == nil {
		a.logger.Error("iOS final text insertion skipped because pipeline is unavailable", "device_id", deviceID)
		return
	}

	go func(text string, pipeline *Pipeline) {
		ctx, cancel := context.WithTimeout(context.Background(), iosInsertTimeout)
		defer cancel()

		insertStartedAt := time.Now()
		receipt, err := pipeline.InsertText(ctx, text)
		a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
			trace.InsertCompletedAt = time.Now()
			trace.InsertMS = durationMilliseconds(time.Since(insertStartedAt))
		})
		if err != nil {
			a.recordLatencyError(traceID, "insert", err)
			var insertErr *inject.InsertError
			if errors.As(err, &insertErr) {
				a.logger.Error("iOS final text insertion failed", "device_id", deviceID, "code", insertErr.Code, "message", insertErr.UserMessage, "error", insertErr.Err)
				return
			}
			a.logger.Error("iOS final text insertion failed", "device_id", deviceID, "error", err)
			return
		}

		a.logger.Info("iOS final text inserted", "trace_id", traceID, "device_id", deviceID, "target", receipt.Target, "status", receipt.Status, "restore_status", receipt.RestoreStatus)
	}(finalText, pipeline)
}

func newLatencyTraceID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	return "trace-" + hex.EncodeToString(raw[:])
}

func (a *App) completeLatencyTrace(traceID string) {
	a.latencyRecorder.Update(traceID, func(trace *LatencyTrace) {
		if trace.CompletedAt.IsZero() {
			trace.CompletedAt = time.Now()
		}
	})
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
	var insertErr *inject.InsertError
	if errors.As(err, &insertErr) {
		return iosAudioWebSocketError{
			Code:    string(insertErr.Code),
			Message: insertErr.UserMessage,
		}
	}
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
		if code := asr.RuntimeErrorCodeOf(err); code != "" {
			diagnostic := asr.RuntimeErrorDiagnosticOf(err)
			return iosAudioWebSocketError{
				Code:       code,
				Message:    iosRuntimeFailureMessage(code, diagnostic, err.Error()),
				Diagnostic: diagnostic,
			}
		}
		return iosAudioWebSocketError{Code: "processing_failed", Message: "audio session could not be processed", Diagnostic: err.Error()}
	}
}

func iosRuntimeFailureMessage(code, diagnostic, fallback string) string {
	lower := strings.ToLower(strings.TrimSpace(diagnostic))
	switch {
	case code == asr.ErrorCodeModelMissing && strings.Contains(lower, "tlg.fst"):
		return "The Mac ASR language model bundle is missing."
	case code == asr.ErrorCodeModelMissing:
		return "The Mac ASR model bundle is incomplete."
	case code == asr.ErrorCodeRuntimeConfigInvalid:
		return "The Mac ASR configuration is invalid."
	case strings.Contains(lower, "address already in use"):
		return "The Mac ASR runtime could not claim a local port."
	case strings.Contains(lower, "did not become healthy within"):
		return "The Mac ASR runtime took too long to become ready."
	case code == asr.ErrorCodeRuntimeStartupFailed:
		return "The Mac ASR runtime could not start."
	case code == asr.ErrorCodeRuntimeUnavailable:
		return "The Mac ASR runtime is unavailable."
	case code == asr.ErrorCodeEmptyTranscript:
		return "The Mac ASR runtime returned an empty transcript."
	default:
		if strings.TrimSpace(fallback) != "" {
			return fallback
		}
		return "audio session could not be processed"
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

func writeIOSWebSocketClose(conn net.Conn, code uint16, reason string) error {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], code)
	copy(payload[2:], reason)

	frame := []byte{0x88}
	if len(payload) < 126 {
		frame = append(frame, byte(len(payload)))
	} else if len(payload) <= 65535 {
		frame = append(frame, 126, byte(len(payload)>>8), byte(len(payload)))
	} else {
		return fmt.Errorf("websocket close payload too large")
	}
	frame = append(frame, payload...)
	_, err := conn.Write(frame)
	return err
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
