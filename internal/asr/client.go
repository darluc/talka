package asr

import (
	"context"
	"encoding/base64"
	"time"

	"talka/internal/protocol"
)

const (
	DefaultURL         = "ws://127.0.0.1:19080/ws"
	defaultSessionID   = "session-1"
	defaultStreamID    = "stream-1"
	quietReadWindow    = 20 * time.Millisecond
	defaultClientName  = "talka-go"
	defaultTimeout     = 2 * time.Second
)

type Config struct {
	URL     string
	Version string
	Timeout time.Duration
}

type Client struct {
	config Config
}

type StreamResult struct {
	Partials  []protocol.ASRPartial
	Finals    []protocol.ASRFinal
	TextFinal protocol.TextFinal
}

func NewClient(config Config) *Client {
	if config.URL == "" {
		config.URL = DefaultURL
	}
	if config.Version == "" {
		config.Version = protocol.VersionV1Alpha1
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}

	return &Client{config: config}
}

func (c *Client) HealthCheck(ctx context.Context) error {
	conn, err := dialWebSocket(ctx, c.config.URL, c.config.Timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.WriteJSON(protocol.ClientHello{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeClientHello}, ClientName: defaultClientName, SessionID: defaultSessionID}); err != nil {
		return err
	}

	msg, err := readMessage(ctx, conn)
	if err != nil {
		return err
	}

	switch typed := msg.(type) {
	case protocol.ServerHello:
		if !typed.Ready {
			return protocol.NewError(protocol.ErrorCodeSidecarUnavailable, "sidecar is not ready")
		}
		return nil
	case protocol.ErrorMessage:
		return typed.AsError()
	default:
		return protocol.NewError(protocol.ErrorCodeUnexpectedMessageType, "health check received %T", msg)
	}
}

func (c *Client) Transcribe(ctx context.Context, metadata protocol.AudioMetadata, frames [][]byte) (StreamResult, error) {
	var result StreamResult

	conn, err := dialWebSocket(ctx, c.config.URL, c.config.Timeout)
	if err != nil {
		return result, err
	}
	defer conn.Close()

	if err := conn.WriteJSON(protocol.ClientHello{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeClientHello}, ClientName: defaultClientName, SessionID: defaultSessionID}); err != nil {
		return result, err
	}

	msg, err := readMessage(ctx, conn)
	if err != nil {
		return result, err
	}
	if err := consumeMessage(msg, &result); err != nil {
		return result, err
	}

	if err := conn.WriteJSON(protocol.AudioStart{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeAudioStart}, SessionID: defaultSessionID, StreamID: defaultStreamID, Metadata: metadata}); err != nil {
		return result, err
	}
	if done, err := drainUntilQuiet(ctx, conn, quietReadWindow, &result); err != nil || done {
		return result, err
	}

	for index, frame := range frames {
		if err := conn.WriteJSON(protocol.AudioFrame{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeAudioFrame}, SessionID: defaultSessionID, StreamID: defaultStreamID, Sequence: index + 1, PayloadBase64: base64.StdEncoding.EncodeToString(frame)}); err != nil {
			return result, err
		}

		if done, err := drainUntilQuiet(ctx, conn, quietReadWindow, &result); err != nil || done {
			return result, err
		}
	}

	if err := conn.WriteJSON(protocol.AudioStop{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeAudioStop}, SessionID: defaultSessionID, StreamID: defaultStreamID, LastSequence: len(frames)}); err != nil {
		return result, err
	}

	for {
		msg, err := readMessage(ctx, conn)
		if err != nil {
			return result, err
		}

		done, err := consumeMessageWithDone(msg, &result)
		if err != nil {
			return result, err
		}
		if done {
			return result, nil
		}
	}
}

func drainUntilQuiet(ctx context.Context, conn *websocketConn, quiet time.Duration, result *StreamResult) (bool, error) {
	for {
		readCtx, cancel := context.WithTimeout(ctx, quiet)
		msg, err := readMessage(readCtx, conn)
		cancel()
		if err != nil {
			if isTimeout(err) {
				return false, nil
			}
			return false, err
		}

		done, err := consumeMessageWithDone(msg, result)
		if err != nil || done {
			return done, err
		}
	}
}

func consumeMessage(msg any, result *StreamResult) error {
	_, err := consumeMessageWithDone(msg, result)
	return err
}

func consumeMessageWithDone(msg any, result *StreamResult) (bool, error) {
	switch typed := msg.(type) {
	case protocol.ServerHello:
		if !typed.Ready {
			return false, protocol.NewError(protocol.ErrorCodeSidecarUnavailable, "sidecar is not ready")
		}
		return false, nil
	case protocol.ASRPartial:
		result.Partials = append(result.Partials, typed)
		return false, nil
	case protocol.ASRFinal:
		result.Finals = append(result.Finals, typed)
		return false, nil
	case protocol.TextFinal:
		result.TextFinal = typed
		return true, nil
	case protocol.ErrorMessage:
		return false, typed.AsError()
	default:
		return false, protocol.NewError(protocol.ErrorCodeUnexpectedMessageType, "unexpected runtime message %T", msg)
	}
}
