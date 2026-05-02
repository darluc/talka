package session

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestDiagnosticAudioCaptureReconstructsWAVFromEncryptedFrames(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	client, server := newLinkedStateMachines(t, clock)
	capture := DiagnosticAudioCapture{Enabled: true}
	frames := [][]byte{
		bytes.Repeat([]byte{1}, 640),
		bytes.Repeat([]byte{2}, 640),
	}

	messages := encryptAudioStream(t, client, frames)
	var result DiagnosticAudioCaptureResult
	var completed bool
	for _, msg := range messages {
		var err error
		result, completed, err = capture.CaptureEncryptedMessage(server, msg)
		if err != nil {
			t.Fatalf("CaptureEncryptedMessage() error = %v", err)
		}
	}

	if !completed {
		t.Fatal("CaptureEncryptedMessage() completed = false, want true after audio_stop")
	}
	if result.FrameCount != len(frames) {
		t.Fatalf("FrameCount = %d, want %d", result.FrameCount, len(frames))
	}
	assertPCM16WAV(t, result.WAV, bytes.Join(frames, nil))
}

func TestDiagnosticAudioCaptureDisabledDoesNotStoreRawAudio(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	client, server := newLinkedStateMachines(t, clock)
	capture := DiagnosticAudioCapture{Enabled: false}
	messages := encryptAudioStream(t, client, [][]byte{bytes.Repeat([]byte{1}, 640)})

	var result DiagnosticAudioCaptureResult
	var completed bool
	for _, msg := range messages {
		var err error
		result, completed, err = capture.CaptureEncryptedMessage(server, msg)
		if err != nil {
			t.Fatalf("CaptureEncryptedMessage() error = %v", err)
		}
	}

	if !completed {
		t.Fatal("CaptureEncryptedMessage() completed = false, want true after audio_stop")
	}
	if result.FrameCount != 1 {
		t.Fatalf("FrameCount = %d, want 1", result.FrameCount)
	}
	if len(result.WAV) != 0 {
		t.Fatalf("WAV length = %d, want 0 with diagnostic capture disabled", len(result.WAV))
	}
}

func encryptAudioStream(t *testing.T, client *StateMachine, frames [][]byte) []EncryptedMessage {
	t.Helper()
	metadata := protocol.AudioMetadata{SampleRate: 16000, Channels: 1, Encoding: protocol.EncodingPCMS16LE, FrameDurationMS: 20, Language: "zh-CN"}
	stream := []any{
		protocol.AudioStart{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStart}, SessionID: "session-1", StreamID: "stream-1", Metadata: metadata},
	}
	for index, frame := range frames {
		stream = append(stream, protocol.AudioFrame{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioFrame}, SessionID: "session-1", StreamID: "stream-1", Sequence: index + 1, PayloadBase64: base64.StdEncoding.EncodeToString(frame)})
	}
	stream = append(stream, protocol.AudioStop{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStop}, SessionID: "session-1", StreamID: "stream-1", LastSequence: len(frames)})

	messages := make([]EncryptedMessage, 0, len(stream))
	for _, event := range stream {
		payload, err := protocol.Encode(event)
		if err != nil {
			t.Fatalf("Encode(%T) error = %v", event, err)
		}
		encrypted, err := client.Encrypt(eventType(event), payload)
		if err != nil {
			t.Fatalf("Encrypt(%T) error = %v", event, err)
		}
		messages = append(messages, encrypted)
	}
	return messages
}

func eventType(event any) protocol.MessageType {
	switch event.(type) {
	case protocol.AudioStart:
		return protocol.MessageTypeAudioStart
	case protocol.AudioFrame:
		return protocol.MessageTypeAudioFrame
	case protocol.AudioStop:
		return protocol.MessageTypeAudioStop
	default:
		return ""
	}
}

func assertPCM16WAV(t *testing.T, wav []byte, wantPCM []byte) {
	t.Helper()
	if len(wav) != 44+len(wantPCM) {
		t.Fatalf("WAV length = %d, want %d", len(wav), 44+len(wantPCM))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("WAV header = %q/%q, want RIFF/WAVE", wav[0:4], wav[8:12])
	}
	if got := binary.LittleEndian.Uint16(wav[20:22]); got != 1 {
		t.Fatalf("WAV audio format = %d, want PCM", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != 1 {
		t.Fatalf("WAV channels = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("WAV sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != 16 {
		t.Fatalf("WAV bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(wantPCM)) {
		t.Fatalf("WAV data bytes = %d, want %d", got, len(wantPCM))
	}
	if !bytes.Equal(wav[44:], wantPCM) {
		t.Fatal("WAV PCM payload does not match decrypted frames")
	}
}
