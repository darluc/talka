package protocol

import "encoding/json"

func Encode(msg any) ([]byte, error) {
	return json.Marshal(msg)
}

func Decode(payload []byte) (any, error) {
	var envelope Envelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, NewError(ErrorCodeInvalidMessage, "decode protocol message: %v", err)
	}

	if envelope.Version != VersionV1Alpha1 {
		return nil, NewError(ErrorCodeInvalidVersion, "unsupported protocol version %q", envelope.Version)
	}

	var target any
	switch envelope.Type {
	case MessageTypeClientHello:
		target = &ClientHello{}
	case MessageTypeServerHello:
		target = &ServerHello{}
	case MessageTypePairingConfirm:
		target = &PairingConfirm{}
	case MessageTypeSessionResume:
		target = &SessionResume{}
	case MessageTypeAudioStart:
		target = &AudioStart{}
	case MessageTypeAudioFrame:
		target = &AudioFrame{}
	case MessageTypeAudioStop:
		target = &AudioStop{}
	case MessageTypeAudioCancel:
		target = &AudioCancel{}
	case MessageTypeASRPartial:
		target = &ASRPartial{}
	case MessageTypeASRFinal:
		target = &ASRFinal{}
	case MessageTypeTextFinal:
		target = &TextFinal{}
	case MessageTypeError:
		target = &ErrorMessage{}
	case MessageTypePing:
		target = &Ping{}
	default:
		return nil, NewError(ErrorCodeUnexpectedMessageType, "unsupported message type %q", envelope.Type)
	}

	if err := json.Unmarshal(payload, target); err != nil {
		return nil, NewError(ErrorCodeInvalidMessage, "decode %s message: %v", envelope.Type, err)
	}

	return dereferenceMessage(target), nil
}

func dereferenceMessage(msg any) any {
	switch typed := msg.(type) {
	case *ClientHello:
		return *typed
	case *ServerHello:
		return *typed
	case *PairingConfirm:
		return *typed
	case *SessionResume:
		return *typed
	case *AudioStart:
		return *typed
	case *AudioFrame:
		return *typed
	case *AudioStop:
		return *typed
	case *AudioCancel:
		return *typed
	case *ASRPartial:
		return *typed
	case *ASRFinal:
		return *typed
	case *TextFinal:
		return *typed
	case *ErrorMessage:
		return *typed
	case *Ping:
		return *typed
	default:
		return msg
	}
}

func ValidateAudioMetadata(metadata AudioMetadata) error {
	if metadata.Encoding != EncodingPCMS16LE {
		return NewError(ErrorCodeUnsupportedEncoding, "encoding %q is not supported", metadata.Encoding)
	}

	if metadata.SampleRate != 16000 || metadata.Channels != 1 || metadata.FrameDurationMS != 20 || metadata.Language == "" {
		return NewError(ErrorCodeInvalidAudioMetadata, "audio metadata must use 16kHz mono 20ms frames with a language")
	}

	return nil
}
