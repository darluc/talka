package asr

import (
	"context"
	"encoding/base64"
	"net/http"

	"talka/internal/protocol"
)

type FakeRuntime struct {
	Ready bool
}

func (r *FakeRuntime) Handler() http.Handler {
	return http.HandlerFunc(r.ServeHTTP)
}

func (r *FakeRuntime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

func (r *FakeRuntime) serveSession(ctx context.Context, conn *websocketConn) {
	state := streamState{expectedSequence: 1, ready: r.Ready}

	for {
		msg, err := readMessage(ctx, conn)
		if err != nil {
			return
		}

		switch typed := msg.(type) {
		case protocol.ClientHello:
			if !state.ready {
				writeProtocolError(conn, protocol.ErrorCodeSidecarUnavailable, "fake runtime is not ready")
				return
			}
			if typed.Version != protocol.VersionV1Alpha1 {
				writeProtocolError(conn, protocol.ErrorCodeInvalidVersion, "unsupported protocol version")
				return
			}
			_ = conn.WriteJSON(protocol.ServerHello{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeServerHello}, RuntimeName: "fake-asr", Ready: true})
		case protocol.AudioStart:
			if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeOf(err), err.Error())
				return
			}
			state.started = true
			state.sessionID = typed.SessionID
			state.streamID = typed.StreamID
		case protocol.AudioFrame:
			if !state.started {
				writeProtocolError(conn, protocol.ErrorCodeInvalidMessage, "audio_start is required before audio_frame")
				return
			}
			if typed.Sequence != state.expectedSequence {
				writeProtocolError(conn, protocol.ErrorCodeOutOfOrderAudioFrame, "audio frames must arrive sequentially")
				return
			}
			if _, err := base64.StdEncoding.DecodeString(typed.PayloadBase64); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeInvalidMessage, "audio frame payload must be valid base64")
				return
			}
			state.expectedSequence++
			state.framesReceived++
			if partialText, ok := fakePartialForFrame(state.framesReceived); ok {
				_ = conn.WriteJSON(protocol.ASRPartial{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeASRPartial}, SessionID: state.sessionID, StreamID: state.streamID, SegmentIndex: state.framesReceived, Text: partialText})
			}
		case protocol.AudioStop:
			finalText := fakeFinalForFrames(state.framesReceived)
			_ = conn.WriteJSON(protocol.ASRFinal{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeASRFinal}, SessionID: state.sessionID, StreamID: state.streamID, SegmentIndex: 1, Text: finalText})
			_ = conn.WriteJSON(protocol.TextFinal{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeTextFinal}, SessionID: state.sessionID, StreamID: state.streamID, Text: finalText})
			return
		case protocol.Ping:
			_ = conn.WriteJSON(protocol.Ping{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypePing}, Nonce: typed.Nonce})
		default:
			writeProtocolError(conn, protocol.ErrorCodeUnexpectedMessageType, "unsupported message for fake runtime")
			return
		}
	}
}

type streamState struct {
	ready              bool
	started            bool
	sessionID          string
	streamID           string
	framesReceived     int
	expectedSequence   int
	finalTexts         []string
	partialTexts       []string
}

func fakePartialForFrame(frameNumber int) (string, bool) {
	switch frameNumber {
	case 1:
		return "你好", true
	case 2:
		return "你好，世界", true
	default:
		return "你好，世界", false
	}
}

func fakeFinalForFrames(frameCount int) string {
	if frameCount < 2 {
		return "你好"
	}
	return "你好，世界"
}

func writeProtocolError(conn *websocketConn, code protocol.ErrorCode, message string) {
	_ = conn.WriteJSON(protocol.ErrorMessage{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeError}, Code: code, Message: message})
}
