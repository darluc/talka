package session

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"talka/internal/protocol"
)

type DiagnosticAudioCapture struct {
	Enabled bool

	metadata   protocol.AudioMetadata
	streamID   string
	started    bool
	lastSeq    int
	frameCount int
	pcm        bytes.Buffer
}

type DiagnosticAudioCaptureResult struct {
	Metadata   protocol.AudioMetadata
	StreamID   string
	FrameCount int
	WAV        []byte
}

func (c *DiagnosticAudioCapture) CaptureEncryptedMessage(machine *StateMachine, msg EncryptedMessage) (DiagnosticAudioCaptureResult, bool, error) {
	plaintext, err := machine.Decrypt(msg)
	if err != nil {
		return DiagnosticAudioCaptureResult{}, false, err
	}
	decoded, err := protocol.Decode(plaintext)
	if err != nil {
		return DiagnosticAudioCaptureResult{}, false, err
	}

	switch typed := decoded.(type) {
	case protocol.AudioStart:
		if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
			return DiagnosticAudioCaptureResult{}, false, err
		}
		c.metadata = typed.Metadata
		c.streamID = typed.StreamID
		c.started = true
		c.lastSeq = 0
		c.frameCount = 0
		c.pcm.Reset()
		return DiagnosticAudioCaptureResult{}, false, nil
	case protocol.AudioFrame:
		if !c.started {
			return DiagnosticAudioCaptureResult{}, false, fmt.Errorf("audio_start is required before audio_frame")
		}
		if typed.StreamID != c.streamID {
			return DiagnosticAudioCaptureResult{}, false, fmt.Errorf("audio frame stream id mismatch")
		}
		if typed.Sequence != c.lastSeq+1 {
			return DiagnosticAudioCaptureResult{}, false, fmt.Errorf("audio frame sequence = %d, want %d", typed.Sequence, c.lastSeq+1)
		}
		payload, err := base64.StdEncoding.DecodeString(typed.PayloadBase64)
		if err != nil {
			return DiagnosticAudioCaptureResult{}, false, err
		}
		if c.Enabled {
			c.pcm.Write(payload)
		}
		c.lastSeq = typed.Sequence
		c.frameCount++
		return DiagnosticAudioCaptureResult{}, false, nil
	case protocol.AudioStop:
		if !c.started {
			return DiagnosticAudioCaptureResult{}, false, fmt.Errorf("audio_start is required before audio_stop")
		}
		if typed.StreamID != c.streamID {
			return DiagnosticAudioCaptureResult{}, false, fmt.Errorf("audio stop stream id mismatch")
		}
		if typed.LastSequence != c.lastSeq {
			return DiagnosticAudioCaptureResult{}, false, fmt.Errorf("last sequence = %d, want %d", typed.LastSequence, c.lastSeq)
		}
		result := DiagnosticAudioCaptureResult{Metadata: c.metadata, StreamID: c.streamID, FrameCount: c.frameCount}
		if c.Enabled {
			result.WAV = encodePCM16WAV(c.pcm.Bytes(), c.metadata)
		}
		return result, true, nil
	default:
		return DiagnosticAudioCaptureResult{}, false, nil
	}
}

func encodePCM16WAV(pcm []byte, metadata protocol.AudioMetadata) []byte {
	dataSize := uint32(len(pcm))
	byteRate := uint32(metadata.SampleRate * metadata.Channels * 2)
	blockAlign := uint16(metadata.Channels * 2)
	out := bytes.NewBuffer(make([]byte, 0, 44+len(pcm)))
	out.WriteString("RIFF")
	writeLE(out, uint32(36)+dataSize)
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	writeLE(out, uint32(16))
	writeLE(out, uint16(1))
	writeLE(out, uint16(metadata.Channels))
	writeLE(out, uint32(metadata.SampleRate))
	writeLE(out, byteRate)
	writeLE(out, blockAlign)
	writeLE(out, uint16(16))
	out.WriteString("data")
	writeLE(out, dataSize)
	out.Write(pcm)
	return out.Bytes()
}

func writeLE(buf *bytes.Buffer, value any) {
	_ = binary.Write(buf, binary.LittleEndian, value)
}
