package asr

import (
	"context"
	"encoding/base64"
	"time"

	"talka/internal/protocol"
)

const (
	DefaultURL        = "ws://127.0.0.1:19080/ws"
	defaultSessionID  = "session-1"
	defaultStreamID   = "stream-1"
	quietReadWindow   = 20 * time.Millisecond
	defaultClientName = "talka-go"
	defaultTimeout    = 2 * time.Second
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

type ClientStream struct {
	client   *Client
	conn     *websocketConn
	result   StreamResult
	metadata protocol.AudioMetadata
	sequence int
	closed   bool
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
	stream, err := c.NewStream(ctx, metadata)
	if err != nil {
		return StreamResult{}, err
	}
	defer stream.Close(ctx)

	for index, frame := range frames {
		if _, err := stream.AcceptFrame(ctx, index+1, frame); err != nil {
			return StreamResult{}, err
		}
	}

	return stream.Finish(ctx)
}

func (c *Client) NewStream(ctx context.Context, metadata protocol.AudioMetadata) (*ClientStream, error) {
	var result StreamResult

	conn, err := dialWebSocket(ctx, c.config.URL, c.config.Timeout)
	if err != nil {
		return nil, err
	}

	stream := &ClientStream{client: c, conn: conn, result: result, metadata: metadata}
	if err := conn.WriteJSON(protocol.ClientHello{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeClientHello}, ClientName: defaultClientName, SessionID: defaultSessionID}); err != nil {
		_ = stream.Close(ctx)
		return nil, err
	}

	msg, err := readMessage(ctx, conn)
	if err != nil {
		_ = stream.Close(ctx)
		return nil, err
	}
	if err := consumeMessage(msg, &stream.result); err != nil {
		_ = stream.Close(ctx)
		return nil, err
	}

	if err := conn.WriteJSON(protocol.AudioStart{Envelope: protocol.Envelope{Version: c.config.Version, Type: protocol.MessageTypeAudioStart}, SessionID: defaultSessionID, StreamID: defaultStreamID, Metadata: metadata}); err != nil {
		_ = stream.Close(ctx)
		return nil, err
	}
	if done, err := drainUntilQuiet(ctx, conn, quietReadWindow, &stream.result); err != nil || done {
		_ = stream.Close(ctx)
		return nil, err
	}

	return stream, nil
}

func (s *ClientStream) AcceptFrame(ctx context.Context, sequence int, frame []byte) (StreamResult, error) {
	if s.closed {
		return s.result, protocol.NewError(protocol.ErrorCodeInvalidMessage, "sidecar stream is closed")
	}
	if sequence <= 0 {
		sequence = s.sequence + 1
	}
	if err := s.conn.WriteJSON(protocol.AudioFrame{Envelope: protocol.Envelope{Version: s.client.config.Version, Type: protocol.MessageTypeAudioFrame}, SessionID: defaultSessionID, StreamID: defaultStreamID, Sequence: sequence, PayloadBase64: base64.StdEncoding.EncodeToString(frame)}); err != nil {
		return s.result, err
	}
	s.sequence = sequence

	if done, err := drainUntilQuiet(ctx, s.conn, quietReadWindow, &s.result); err != nil || done {
		return s.result, err
	}
	return s.result, nil
}

func (s *ClientStream) Finish(ctx context.Context) (StreamResult, error) {
	if s.closed {
		return s.result, protocol.NewError(protocol.ErrorCodeInvalidMessage, "sidecar stream is closed")
	}
	if err := s.conn.WriteJSON(protocol.AudioStop{Envelope: protocol.Envelope{Version: s.client.config.Version, Type: protocol.MessageTypeAudioStop}, SessionID: defaultSessionID, StreamID: defaultStreamID, LastSequence: s.sequence}); err != nil {
		return s.result, err
	}

	for {
		msg, err := readMessage(ctx, s.conn)
		if err != nil {
			return s.result, err
		}

		done, err := consumeMessageWithDone(msg, &s.result)
		if err != nil {
			return s.result, err
		}
		if done {
			return s.result, nil
		}
	}
}

func (s *ClientStream) Close(ctx context.Context) error {
	_ = ctx
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close()
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
