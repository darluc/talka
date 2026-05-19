package protocol

const VersionV1Alpha1 = "v1alpha1"

type MessageType string

const (
	MessageTypeClientHello    MessageType = "client_hello"
	MessageTypeServerHello    MessageType = "server_hello"
	MessageTypePairingConfirm MessageType = "pairing_confirm"
	MessageTypeSessionResume  MessageType = "session_resume"
	MessageTypeAudioStart     MessageType = "audio_start"
	MessageTypeAudioFrame     MessageType = "audio_frame"
	MessageTypeAudioStop      MessageType = "audio_stop"
	MessageTypeAudioCancel    MessageType = "audio_cancel"
	MessageTypeKeyPress       MessageType = "key_press"
	MessageTypeASRPartial     MessageType = "asr_partial"
	MessageTypeASRFinal       MessageType = "asr_final"
	MessageTypeTextFinal      MessageType = "text_final"
	MessageTypeError          MessageType = "error"
	MessageTypePing           MessageType = "ping"
)

const EncodingPCMS16LE = "pcm_s16le"

type Envelope struct {
	Version string      `json:"version"`
	Type    MessageType `json:"type"`
}

type AudioMetadata struct {
	SampleRate      int    `json:"sample_rate"`
	Channels        int    `json:"channels"`
	Encoding        string `json:"encoding"`
	FrameDurationMS int    `json:"frame_duration_ms"`
	Language        string `json:"language"`
}

type ClientHello struct {
	Envelope
	ClientName string `json:"client_name"`
	SessionID  string `json:"session_id,omitempty"`
}

type ServerHello struct {
	Envelope
	RuntimeName string `json:"runtime_name"`
	Ready       bool   `json:"ready"`
}

type PairingConfirm struct {
	Envelope
	SessionID string `json:"session_id"`
	PairingID string `json:"pairing_id"`
}

type SessionResume struct {
	Envelope
	SessionID string `json:"session_id"`
}

type AudioStart struct {
	Envelope
	SessionID string        `json:"session_id"`
	StreamID  string        `json:"stream_id"`
	Metadata  AudioMetadata `json:"metadata"`
}

type AudioFrame struct {
	Envelope
	SessionID     string `json:"session_id"`
	StreamID      string `json:"stream_id"`
	Sequence      int    `json:"sequence"`
	PayloadBase64 string `json:"payload_b64"`
}

type AudioStop struct {
	Envelope
	SessionID    string `json:"session_id"`
	StreamID     string `json:"stream_id"`
	LastSequence int    `json:"last_sequence"`
}

type AudioCancel struct {
	Envelope
	SessionID string `json:"session_id"`
	StreamID  string `json:"stream_id"`
	Reason    string `json:"reason"`
}

type KeyModifier string

const (
	KeyModifierCommand KeyModifier = "cmd"
	KeyModifierAlt     KeyModifier = "alt"
	KeyModifierShift   KeyModifier = "shift"
)

type KeyPress struct {
	Envelope
	SessionID string        `json:"session_id"`
	Key       string        `json:"key"`
	Modifiers []KeyModifier `json:"modifiers,omitempty"`
}

type ASRPartial struct {
	Envelope
	SessionID    string `json:"session_id"`
	StreamID     string `json:"stream_id"`
	SegmentIndex int    `json:"segment_index"`
	Text         string `json:"text"`
}

type ASRFinal struct {
	Envelope
	SessionID    string `json:"session_id"`
	StreamID     string `json:"stream_id"`
	SegmentIndex int    `json:"segment_index"`
	Text         string `json:"text"`
}

type TextFinal struct {
	Envelope
	SessionID string `json:"session_id"`
	StreamID  string `json:"stream_id"`
	Text      string `json:"text"`
}

type ErrorCode string

const (
	ErrorCodeInvalidVersion        ErrorCode = "invalid_version"
	ErrorCodeUnsupportedEncoding   ErrorCode = "unsupported_encoding"
	ErrorCodeInvalidAudioMetadata  ErrorCode = "invalid_audio_metadata"
	ErrorCodeOutOfOrderAudioFrame  ErrorCode = "out_of_order_audio_frame"
	ErrorCodeSidecarUnavailable    ErrorCode = "sidecar_unavailable"
	ErrorCodeUnexpectedMessageType ErrorCode = "unexpected_message_type"
	ErrorCodeInvalidMessage        ErrorCode = "invalid_message"
)

type ErrorMessage struct {
	Envelope
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type Ping struct {
	Envelope
	Nonce string `json:"nonce"`
}
