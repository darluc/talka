package protocol

import (
	"testing"
)

func TestDecodeRoundTripsAllTransportMessages(t *testing.T) {
	tests := []struct {
		name string
		msg  any
	}{
		{name: "client hello", msg: ClientHello{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeClientHello}, ClientName: "ios-client", SessionID: "session-1"}},
		{name: "server hello", msg: ServerHello{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeServerHello}, RuntimeName: "fake-asr", Ready: true}},
		{name: "pairing confirm", msg: PairingConfirm{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypePairingConfirm}, SessionID: "session-1", PairingID: "pair-1"}},
		{name: "session resume", msg: SessionResume{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeSessionResume}, SessionID: "session-1"}},
		{name: "audio start", msg: AudioStart{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeAudioStart}, SessionID: "session-1", StreamID: "stream-1", Metadata: AudioMetadata{SampleRate: 16000, Channels: 1, Encoding: EncodingPCMS16LE, FrameDurationMS: 20, Language: "zh-CN"}}},
		{name: "audio frame", msg: AudioFrame{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeAudioFrame}, SessionID: "session-1", StreamID: "stream-1", Sequence: 1, PayloadBase64: "AAECAw=="}},
		{name: "audio stop", msg: AudioStop{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeAudioStop}, SessionID: "session-1", StreamID: "stream-1", LastSequence: 2}},
		{name: "audio cancel", msg: AudioCancel{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeAudioCancel}, SessionID: "session-1", StreamID: "stream-1", Reason: "user_cancelled"}},
		{name: "asr partial", msg: ASRPartial{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeASRPartial}, SessionID: "session-1", StreamID: "stream-1", SegmentIndex: 1, Text: "你好"}},
		{name: "asr final", msg: ASRFinal{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeASRFinal}, SessionID: "session-1", StreamID: "stream-1", SegmentIndex: 1, Text: "你好，世界"}},
		{name: "text final", msg: TextFinal{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeTextFinal}, SessionID: "session-1", StreamID: "stream-1", Text: "你好，世界"}},
		{name: "error", msg: ErrorMessage{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypeError}, Code: ErrorCodeUnsupportedEncoding, Message: "encoding not supported"}},
		{name: "ping", msg: Ping{Envelope: Envelope{Version: VersionV1Alpha1, Type: MessageTypePing}, Nonce: "ping-1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := Encode(tc.msg)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			decoded, err := Decode(payload)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			if got, want := messageTypeOf(decoded), messageTypeOf(tc.msg); got != want {
				t.Fatalf("message type = %q, want %q", got, want)
			}
		})
	}
}

func TestDecodeRejectsInvalidVersion(t *testing.T) {
	payload := []byte(`{"version":"v0","type":"client_hello","client_name":"ios-client"}`)

	_, err := Decode(payload)
	if err == nil {
		t.Fatal("Decode() error = nil, want invalid version error")
	}

	if got, want := ErrorCodeOf(err), ErrorCodeInvalidVersion; got != want {
		t.Fatalf("ErrorCodeOf() = %q, want %q", got, want)
	}
}

func messageTypeOf(msg any) MessageType {
	switch typed := msg.(type) {
	case ClientHello:
		return typed.Type
	case ServerHello:
		return typed.Type
	case PairingConfirm:
		return typed.Type
	case SessionResume:
		return typed.Type
	case AudioStart:
		return typed.Type
	case AudioFrame:
		return typed.Type
	case AudioStop:
		return typed.Type
	case AudioCancel:
		return typed.Type
	case ASRPartial:
		return typed.Type
	case ASRFinal:
		return typed.Type
	case TextFinal:
		return typed.Type
	case ErrorMessage:
		return typed.Type
	case Ping:
		return typed.Type
	default:
		return ""
	}
}
