package asr

import (
	"context"
	"net/http"

	"talka/internal/protocol"
)

const crashRuntimeExitCode = 86

type CrashRuntime struct {
	Exit func(int)
}

func (r *CrashRuntime) Handler() http.Handler {
	return http.HandlerFunc(r.ServeHTTP)
}

func (r *CrashRuntime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

func (r *CrashRuntime) serveSession(ctx context.Context, conn *websocketConn) {
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
			_ = conn.WriteJSON(protocol.ServerHello{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeServerHello}, RuntimeName: "asr-crash", Ready: true})
		case protocol.AudioStart:
			if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeOf(err), err.Error())
				return
			}
		case protocol.AudioFrame:
			if r.Exit != nil {
				r.Exit(crashRuntimeExitCode)
			}
			return
		default:
			writeProtocolError(conn, protocol.ErrorCodeUnexpectedMessageType, "unsupported message for crash runtime")
			return
		}
	}
}
