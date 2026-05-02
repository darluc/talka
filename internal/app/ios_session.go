package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"talka/internal/pairing"
	"talka/internal/protocol"
	"talka/internal/session"
)

const iosSecureTransportUnavailableMessage = "iOS secure pairing transport is unavailable until the X25519/HMAC pairing handshake is wired to production."

type iosAudioSession struct {
	deviceID string
	machine  *session.StateMachine
}

func (a *App) handleIOSPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "iOS pairing endpoint only accepts POST", nil)
		return
	}
	var request IOSPairingCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "decode iOS pairing payload", nil)
		return
	}
	result, err := a.completeIOSPairing(r, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "pairing_failed", "pairing completion failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleIOSPairingChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "iOS pairing challenge endpoint only accepts GET", nil)
		return
	}
	a.mu.RLock()
	start := a.iosPairing
	a.mu.RUnlock()
	if start == nil {
		writeError(w, http.StatusNotFound, "pairing_not_active", "no active pairing PIN", nil)
		return
	}
	writeJSON(w, http.StatusOK, IOSPairingChallengeResponse{PairingID: start.PairingID, ExpiresAt: start.ExpiresAt, ServerDeviceID: a.serverDeviceID(), ServerDeviceName: a.serverDeviceName(), ServerIdentityPublicKey: base64.StdEncoding.EncodeToString(start.ServerIdentityPublicKey), ServerEphemeralPublicKey: base64.StdEncoding.EncodeToString(start.ServerEphemeralPublicKey)})
}

func (a *App) handleIOSResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "iOS resume endpoint only accepts POST", nil)
		return
	}
	var request IOSResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		a.logger.Error("failed to decode iOS resume payload", "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid iOS resume payload", nil)
		return
	}
	result, err := a.resumeIOSPairing(r, request)
	if err != nil {
		a.logger.Error("iOS resume failed", "error", err)
		writeError(w, http.StatusBadRequest, "resume_failed", "iOS resume failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) completeIOSPairing(r *http.Request, request IOSPairingCompleteRequest) (IOSPairingResponse, error) {
	clientIdentity, err := decodeRequiredBase64(request.ClientIdentityPublicKey, "client_identity_public_key")
	if err != nil {
		return IOSPairingResponse{}, err
	}
	clientEphemeral, err := decodeRequiredBase64(request.ClientEphemeralPublicKey, "client_ephemeral_public_key")
	if err != nil {
		return IOSPairingResponse{}, err
	}
	confirmation, err := decodeRequiredBase64(request.ClientConfirmation, "client_confirmation")
	if err != nil {
		return IOSPairingResponse{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.iosManager.Complete(pairing.PairingRequest{PairingID: strings.TrimSpace(request.PairingID), ClientDeviceID: strings.TrimSpace(request.DeviceID), ClientDeviceName: strings.TrimSpace(request.DeviceName), ClientIdentityPublicKey: clientIdentity, ClientEphemeralPublicKey: clientEphemeral, ClientConfirmation: confirmation})
	if err != nil {
		return IOSPairingResponse{}, err
	}
	a.devices[result.ClientDeviceID] = Device{ID: result.ClientDeviceID, Name: result.ClientDeviceName, Paired: true, LastSeenAt: result.Device.LastSeenAt}
	a.iosSessions[result.ClientDeviceID] = &iosAudioSession{deviceID: result.ClientDeviceID, machine: result.ServerSession}
	a.pairing = nil
	a.iosPairing = nil
	return iosPairingResponseFromResult(r, result.ClientDeviceID, result.ClientDeviceName, result.ServerDeviceID, result.ServerDeviceName, result.ServerIdentityPublicKey, result.ServerEphemeralPublicKey, result.Confirmation, result.SessionID), nil
}

func (a *App) resumeIOSPairing(r *http.Request, request IOSResumeRequest) (IOSPairingResponse, error) {
	clientIdentity, err := decodeRequiredBase64(request.ClientIdentityPublicKey, "client_identity_public_key")
	if err != nil {
		return IOSPairingResponse{}, err
	}
	clientEphemeral, err := decodeRequiredBase64(request.ClientEphemeralPublicKey, "client_ephemeral_public_key")
	if err != nil {
		return IOSPairingResponse{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.iosManager.Resume(pairing.ResumeRequest{PairingID: strings.TrimSpace(request.PairingID), ClientDeviceID: strings.TrimSpace(request.DeviceID), ClientDeviceName: strings.TrimSpace(request.DeviceName), ClientIdentityPublicKey: clientIdentity, ClientEphemeralPublicKey: clientEphemeral})
	if err != nil {
		return IOSPairingResponse{}, err
	}
	a.devices[result.ClientDeviceID] = Device{ID: result.ClientDeviceID, Name: result.ClientDeviceName, Paired: true, LastSeenAt: result.Device.LastSeenAt}
	a.iosSessions[result.ClientDeviceID] = &iosAudioSession{deviceID: result.ClientDeviceID, machine: result.ServerSession}
	return iosPairingResponseFromResult(r, result.ClientDeviceID, result.ClientDeviceName, result.ServerDeviceID, result.ServerDeviceName, result.ServerIdentityPublicKey, result.ServerEphemeralPublicKey, result.Confirmation, result.SessionID), nil
}

func (a *App) ProcessEncryptedIOSAudioSession(ctx context.Context, deviceID string, messages []session.EncryptedMessage) (ProcessResult, error) {
	a.mu.Lock()
	active := a.iosSessions[deviceID]
	pipeline := a.pipeline
	if active == nil {
		a.mu.Unlock()
		return ProcessResult{}, fmt.Errorf("no active audio session for device %q", deviceID)
	}
	if pipeline == nil {
		a.mu.Unlock()
		return ProcessResult{}, fmt.Errorf("audio pipeline is not configured")
	}
	metadata, frames, err := decryptIOSAudioMessages(active.machine, messages)
	a.mu.Unlock()
	if err != nil {
		a.logger.Error("iOS audio session decrypt failed", "device_id", deviceID, "encrypted_messages", len(messages), "error", err)
		return ProcessResult{}, err
	}
	a.logger.Info("iOS audio session decrypted", "device_id", deviceID, "encrypted_messages", len(messages), "frames", len(frames), "sample_rate", metadata.SampleRate, "channels", metadata.Channels, "encoding", metadata.Encoding, "frame_duration_ms", metadata.FrameDurationMS)
	result, err := pipeline.ProcessAudioFrames(ctx, metadata, frames)
	if err != nil {
		a.logger.Error("iOS audio pipeline failed", "device_id", deviceID, "frames", len(frames), "error", err)
		return ProcessResult{}, err
	}
	a.logger.Info("iOS audio pipeline completed", "device_id", deviceID, "frames", len(frames), "raw_transcript_chars", len(result.RawTranscript), "final_text_chars", len(result.FinalText))
	return result, nil
}

func iosPairingResponseFromResult(r *http.Request, deviceID, deviceName, serverDeviceID, serverDeviceName string, serverIdentityPublicKey, serverEphemeralPublicKey, confirmation, sessionID []byte) IOSPairingResponse {
	return IOSPairingResponse{DeviceID: deviceID, DeviceName: deviceName, ServerDeviceID: serverDeviceID, ServerDeviceName: serverDeviceName, ServerIdentityPublicKey: base64.StdEncoding.EncodeToString(serverIdentityPublicKey), ServerEphemeralPublicKey: base64.StdEncoding.EncodeToString(serverEphemeralPublicKey), ServerConfirmation: base64.StdEncoding.EncodeToString(confirmation), SessionID: base64.StdEncoding.EncodeToString(sessionID), AudioWebSocketURL: audioWebSocketURL(r, deviceID)}
}

func decodeRequiredBase64(value, field string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%s is required", field)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	return decoded, nil
}

func (a *App) serverDeviceID() string {
	return "talka-mac"
}

func (a *App) serverDeviceName() string {
	return a.cfg.Server.ServiceName
}

func audioWebSocketURL(r *http.Request, deviceID string) string {
	scheme := "ws"
	if r.TLS != nil {
		scheme = "wss"
	}
	return scheme + "://" + r.Host + "/v1/session/audio?device_id=" + deviceID
}

func decryptIOSAudioMessages(machine *session.StateMachine, messages []session.EncryptedMessage) (protocol.AudioMetadata, [][]byte, error) {
	var metadata protocol.AudioMetadata
	var streamID string
	var frames [][]byte
	started := false
	stopped := false

	for _, message := range messages {
		plaintext, err := machine.Decrypt(message)
		if err != nil {
			return protocol.AudioMetadata{}, nil, err
		}
		decoded, err := protocol.Decode(plaintext)
		if err != nil {
			return protocol.AudioMetadata{}, nil, err
		}
		switch typed := decoded.(type) {
		case protocol.AudioStart:
			if started {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("duplicate audio_start")
			}
			if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
				return protocol.AudioMetadata{}, nil, err
			}
			metadata = typed.Metadata
			streamID = typed.StreamID
			started = true
		case protocol.AudioFrame:
			if !started {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("audio_start is required before audio_frame")
			}
			if typed.StreamID != streamID {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("audio frame stream id mismatch")
			}
			if typed.Sequence != len(frames)+1 {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("audio frame sequence = %d, want %d", typed.Sequence, len(frames)+1)
			}
			payload, err := base64.StdEncoding.DecodeString(typed.PayloadBase64)
			if err != nil {
				return protocol.AudioMetadata{}, nil, err
			}
			frames = append(frames, payload)
		case protocol.AudioStop:
			if !started {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("audio_start is required before audio_stop")
			}
			if typed.StreamID != streamID {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("audio stop stream id mismatch")
			}
			if typed.LastSequence != len(frames) {
				return protocol.AudioMetadata{}, nil, fmt.Errorf("last sequence = %d, want %d", typed.LastSequence, len(frames))
			}
			stopped = true
		default:
			return protocol.AudioMetadata{}, nil, fmt.Errorf("unexpected message type %T", decoded)
		}
	}
	if !stopped {
		return protocol.AudioMetadata{}, nil, fmt.Errorf("audio_stop is required")
	}
	return metadata, frames, nil
}
