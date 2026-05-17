package asr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"talka/internal/protocol"
)

const upstreamQuietWindow = 20 * time.Millisecond

const upstreamFinalizationTimeout = 30 * time.Second

const funASRWebSocketSubprotocol = "binary"

type ProxyRuntimeConfig struct {
	UpstreamURL string
	Mode        string
}

type ProxyRuntime struct {
	config ProxyRuntimeConfig
}

type funASRStartMessage struct {
	Mode       string `json:"mode"`
	WavName    string `json:"wav_name"`
	WavFormat  string `json:"wav_format"`
	IsSpeaking bool   `json:"is_speaking"`
	ChunkSize  []int  `json:"chunk_size,omitempty"`
	ITN        bool   `json:"itn"`
}

type funASRResponse struct {
	Mode    string `json:"mode"`
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
}

func NewProxyRuntime(config ProxyRuntimeConfig) *ProxyRuntime {
	if strings.TrimSpace(config.Mode) == "" {
		config.Mode = "2pass"
	}
	return &ProxyRuntime{config: config}
}

func (r *ProxyRuntime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/ws" {
		http.NotFound(w, req)
		return
	}

	conn, err := acceptWebSocket(w, req)
	if err != nil {
		return
	}
	defer conn.Close()

	r.serveSession(req.Context(), conn)
}

func (r *ProxyRuntime) Handler() http.Handler {
	return http.HandlerFunc(r.ServeHTTP)
}

func (r *ProxyRuntime) serveSession(ctx context.Context, conn *websocketConn) {
	state := streamState{expectedSequence: 1}
	var upstream *websocketConn
	defer func() {
		if upstream != nil {
			_ = upstream.Close()
		}
	}()

	for {
		msg, err := readMessage(ctx, conn)
		if err != nil {
			return
		}

		switch typed := msg.(type) {
		case protocol.ClientHello:
			if typed.Version != protocol.VersionV1Alpha1 {
				writeProtocolError(conn, protocol.ErrorCodeInvalidVersion, "unsupported protocol version")
				return
			}
			if err := r.checkUpstreamReady(ctx); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR runtime unavailable: "+err.Error())
				return
			}
			_ = conn.WriteJSON(protocol.ServerHello{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeServerHello}, RuntimeName: "funasr-proxy", Ready: true})
		case protocol.AudioStart:
			if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeOf(err), err.Error())
				return
			}
			state.started = true
			state.sessionID = typed.SessionID
			state.streamID = typed.StreamID
			upstream, err = r.connectUpstream(ctx, typed)
			if err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR runtime unavailable: "+err.Error())
				return
			}
		case protocol.AudioFrame:
			if !state.started || upstream == nil {
				writeProtocolError(conn, protocol.ErrorCodeInvalidMessage, "audio_start is required before audio_frame")
				return
			}
			if typed.Sequence != state.expectedSequence {
				writeProtocolError(conn, protocol.ErrorCodeOutOfOrderAudioFrame, "audio frames must arrive sequentially")
				return
			}
			frame, err := base64.StdEncoding.DecodeString(typed.PayloadBase64)
			if err != nil {
				writeProtocolError(conn, protocol.ErrorCodeInvalidMessage, "audio frame payload must be valid base64")
				return
			}
			if err := upstream.WriteBinary(frame); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR write failed: "+err.Error())
				return
			}
			state.expectedSequence++
			if err := r.drainUpstream(ctx, upstream, conn, &state); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR read failed: "+err.Error())
				return
			}
		case protocol.AudioStop:
			if upstream == nil {
				writeProtocolError(conn, protocol.ErrorCodeInvalidMessage, "audio_start is required before audio_stop")
				return
			}
			if err := upstream.WriteJSON(map[string]any{"is_speaking": false}); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR stop failed: "+err.Error())
				return
			}
			if err := upstream.WriteBinary(nil); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR finalization flush failed: "+err.Error())
				return
			}
			if err := r.awaitFinal(ctx, upstream, conn, &state); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "upstream FunASR finalization failed: "+err.Error())
			}
			return
		default:
			writeProtocolError(conn, protocol.ErrorCodeUnexpectedMessageType, "unsupported message for FunASR proxy runtime")
			return
		}
	}
}

func (r *ProxyRuntime) checkUpstreamReady(ctx context.Context) error {
	conn, err := dialFunASRWebSocket(ctx, r.config.UpstreamURL, defaultTimeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (r *ProxyRuntime) connectUpstream(ctx context.Context, start protocol.AudioStart) (*websocketConn, error) {
	conn, err := dialFunASRWebSocket(ctx, r.config.UpstreamURL, defaultTimeout)
	if err != nil {
		return nil, err
	}

	msg := funASRStartMessage{
		Mode:       r.config.Mode,
		WavName:    start.StreamID,
		WavFormat:  "pcm",
		IsSpeaking: true,
		ITN:        true,
	}
	if r.config.Mode == "2pass" {
		msg.ChunkSize = []int{5, 10, 5}
	}

	if err := conn.WriteJSON(msg); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func dialFunASRWebSocket(ctx context.Context, rawURL string, timeout time.Duration) (*websocketConn, error) {
	return dialWebSocketWithSubprotocols(ctx, rawURL, timeout, []string{funASRWebSocketSubprotocol})
}

func (r *ProxyRuntime) drainUpstream(ctx context.Context, upstream, downstream *websocketConn, state *streamState) error {
	for {
		readCtx, cancel := context.WithTimeout(ctx, upstreamQuietWindow)
		payload, opcode, err := upstream.readFrameWithOpcode(readCtx)
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
		if err := r.forwardTranscript(payload, downstream, state); err != nil {
			return err
		}
	}
}

func (r *ProxyRuntime) awaitFinal(ctx context.Context, upstream, downstream *websocketConn, state *streamState) error {
	deadline := time.Now().Add(upstreamFinalizationTimeout)
	for {
		if time.Now().After(deadline) {
			text := bestFinalText(state)
			if text != "" {
				return downstream.WriteJSON(protocol.TextFinal{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeTextFinal}, SessionID: state.sessionID, StreamID: state.streamID, Text: text})
			}
			return context.DeadlineExceeded
		}

		readCtx, cancel := context.WithTimeout(ctx, upstreamQuietWindow)
		payload, opcode, err := upstream.readFrameWithOpcode(readCtx)
		cancel()
		if err != nil {
			if isTimeout(err) {
				text := committedTranscript(state)
				if text != "" {
					return downstream.WriteJSON(protocol.TextFinal{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeTextFinal}, SessionID: state.sessionID, StreamID: state.streamID, Text: text})
				}
				continue
			}
			if errors.Is(err, io.EOF) {
				text := bestFinalText(state)
				return downstream.WriteJSON(protocol.TextFinal{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeTextFinal}, SessionID: state.sessionID, StreamID: state.streamID, Text: text})
			}
			return err
		}
		if opcode != 0x1 {
			continue
		}
		if err := r.forwardTranscript(payload, downstream, state); err != nil {
			return err
		}
	}
}

// bestFinalText returns the best available final text from the stream state.
// FunASR's offline mode returns is_final:false for the complete transcription,
// so we fall back to partial texts when no explicit final texts were received.
func bestFinalText(state *streamState) string {
	segments := append([]string(nil), state.committedTexts...)
	if state.pendingPartialText != "" {
		segments = append(segments, state.pendingPartialText)
	}
	if len(segments) > 0 {
		return strings.TrimSpace(strings.Join(segments, ""))
	}
	if len(state.finalTexts) > 0 {
		return strings.TrimSpace(strings.Join(state.finalTexts, ""))
	}
	if len(state.partialTexts) > 0 {
		return strings.TrimSpace(state.partialTexts[len(state.partialTexts)-1])
	}
	return ""
}

func committedTranscript(state *streamState) string {
	if len(state.committedTexts) > 0 {
		return strings.TrimSpace(strings.Join(state.committedTexts, ""))
	}
	if len(state.finalTexts) > 0 {
		return strings.TrimSpace(strings.Join(state.finalTexts, ""))
	}
	return ""
}

func (r *ProxyRuntime) forwardTranscript(payload []byte, downstream *websocketConn, state *streamState) error {
	var response funASRResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		if response.IsFinal {
			return nil
		}
		return nil
	}
	if response.IsFinal {
		if state.pendingPartialText != "" && !strings.Contains(text, state.pendingPartialText) {
			state.committedTexts = append(state.committedTexts, state.pendingPartialText)
		}
		state.pendingPartialText = ""
		state.committedTexts = append(state.committedTexts, text)
		state.finalTexts = append(state.finalTexts, text)
		return downstream.WriteJSON(protocol.ASRFinal{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeASRFinal}, SessionID: state.sessionID, StreamID: state.streamID, SegmentIndex: len(state.finalTexts), Text: text})
	}
	state.framesReceived++
	state.partialTexts = append(state.partialTexts, text)
	if state.pendingPartialText != "" && !strings.Contains(text, state.pendingPartialText) && !strings.Contains(state.pendingPartialText, text) {
		state.committedTexts = append(state.committedTexts, state.pendingPartialText)
	}
	state.pendingPartialText = text
	return downstream.WriteJSON(protocol.ASRPartial{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeASRPartial}, SessionID: state.sessionID, StreamID: state.streamID, SegmentIndex: state.framesReceived, Text: text})
}
