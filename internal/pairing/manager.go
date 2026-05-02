package pairing

import (
	"bytes"
	crand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"sync"
	"time"

	intcrypto "talka/internal/crypto"
	"talka/internal/session"
)

type Config struct {
	ServerDeviceID    string
	ServerDeviceName  string
	Store             Store
	Now               func() time.Time
	Rand              io.Reader
	PairingTTL        time.Duration
	InactivityTimeout time.Duration
	MaxFailures       int
}

type Manager struct {
	config  Config
	mu      sync.Mutex
	pending *pendingPairing
}

type pendingPairing struct {
	PairingID       string
	PIN             string
	ExpiresAt       time.Time
	Failures        int
	ServerIdentity  intcrypto.KeyPair
	ServerEphemeral intcrypto.KeyPair
}

type StartResponse struct {
	PairingID                string
	PIN                      string
	ExpiresAt                time.Time
	ServerIdentityPublicKey  []byte
	ServerEphemeralPublicKey []byte
}

type PairingRequest struct {
	PairingID                string
	ClientDeviceID           string
	ClientDeviceName         string
	ClientIdentityPublicKey  []byte
	ClientEphemeralPublicKey []byte
	ClientConfirmation       []byte
}

type PairingResult struct {
	PairingID                string
	SessionID                []byte
	ClientDeviceID           string
	ClientDeviceName         string
	ServerDeviceID           string
	ServerDeviceName         string
	ServerIdentityPublicKey  []byte
	ServerEphemeralPublicKey []byte
	Confirmation             []byte
	ServerSession            *session.StateMachine
	Device                   TrustedDevice
}

type ResumeRequest struct {
	PairingID                string
	ClientDeviceID           string
	ClientDeviceName         string
	ClientIdentityPublicKey  []byte
	ClientEphemeralPublicKey []byte
}

type ResumeResult struct {
	PairingID                string
	SessionID                []byte
	ClientDeviceID           string
	ClientDeviceName         string
	ServerDeviceID           string
	ServerDeviceName         string
	ServerIdentityPublicKey  []byte
	ServerEphemeralPublicKey []byte
	Confirmation             []byte
	ServerSession            *session.StateMachine
	Device                   TrustedDevice
}

func NewManager(config Config) *Manager {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Rand == nil {
		config.Rand = crand.Reader
	}
	if config.PairingTTL <= 0 {
		config.PairingTTL = 2 * time.Minute
	}
	if config.InactivityTimeout <= 0 {
		config.InactivityTimeout = 30 * time.Second
	}
	if config.MaxFailures <= 0 {
		config.MaxFailures = 3
	}
	return &Manager{config: config}
}

func (m *Manager) Start() (StartResponse, error) {
	serverIdentity, err := loadOrCreateLocalIdentity(m.config.Store, m.config.Rand)
	if err != nil {
		return StartResponse{}, err
	}
	serverEphemeral, err := intcrypto.GenerateX25519KeyPair(m.config.Rand)
	if err != nil {
		return StartResponse{}, err
	}
	pairingID, err := randomHex(m.config.Rand, 8)
	if err != nil {
		return StartResponse{}, err
	}
	pin, err := randomPIN(m.config.Rand)
	if err != nil {
		return StartResponse{}, err
	}
	start := StartResponse{
		PairingID:                pairingID,
		PIN:                      pin,
		ExpiresAt:                m.config.Now().Add(m.config.PairingTTL),
		ServerIdentityPublicKey:  bytes.Clone(serverIdentity.Public),
		ServerEphemeralPublicKey: bytes.Clone(serverEphemeral.Public),
	}
	m.mu.Lock()
	m.pending = &pendingPairing{
		PairingID:       pairingID,
		PIN:             pin,
		ExpiresAt:       start.ExpiresAt,
		ServerIdentity:  serverIdentity,
		ServerEphemeral: serverEphemeral,
	}
	m.mu.Unlock()
	return start, nil
}

func (m *Manager) Complete(request PairingRequest) (PairingResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pending == nil || request.PairingID != m.pending.PairingID {
		return PairingResult{}, ErrInvalidPIN
	}
	if m.pending.Failures >= m.config.MaxFailures {
		return PairingResult{}, ErrRateLimited
	}
	if m.config.Now().After(m.pending.ExpiresAt) {
		return PairingResult{}, ErrExpiredPIN
	}
	if err := validatePublicKey(request.ClientIdentityPublicKey); err != nil {
		return PairingResult{}, err
	}
	if err := validatePublicKey(request.ClientEphemeralPublicKey); err != nil {
		return PairingResult{}, err
	}

	transcript := pairingTranscript(
		request.PairingID,
		request.ClientDeviceID,
		request.ClientDeviceName,
		m.config.ServerDeviceID,
		m.config.ServerDeviceName,
		request.ClientIdentityPublicKey,
		request.ClientEphemeralPublicKey,
		m.pending.ServerIdentity.Public,
		m.pending.ServerEphemeral.Public,
	)
	ee, err := intcrypto.ComputeSharedSecret(m.pending.ServerEphemeral.Private, request.ClientEphemeralPublicKey)
	if err != nil {
		return PairingResult{}, err
	}
	ss, err := intcrypto.ComputeSharedSecret(m.pending.ServerIdentity.Private, request.ClientIdentityPublicKey)
	if err != nil {
		return PairingResult{}, err
	}
	expectedConfirmation, err := intcrypto.ComputePairingConfirmation([][]byte{ee, ss}, m.pending.PIN, transcript)
	if err != nil {
		return PairingResult{}, err
	}
	if subtle.ConstantTimeCompare(request.ClientConfirmation, expectedConfirmation) != 1 {
		m.pending.Failures++
		if m.pending.Failures >= m.config.MaxFailures {
			return PairingResult{}, ErrRateLimited
		}
		return PairingResult{}, ErrInvalidPIN
	}

	sessionID, err := intcrypto.RandomSessionID(m.config.Rand)
	if err != nil {
		return PairingResult{}, err
	}
	keys, err := intcrypto.DeriveSessionKeys([][]byte{ee, ss}, sessionID, transcript)
	if err != nil {
		return PairingResult{}, err
	}
	serverSession, err := session.NewStateMachine(session.Config{SessionID: sessionID, SendKey: keys.ServerToClientKey, ReceiveKey: keys.ClientToServerKey, InactivityTimeout: m.config.InactivityTimeout, Now: m.config.Now, Rand: m.config.Rand})
	if err != nil {
		return PairingResult{}, err
	}
	device := TrustedDevice{DeviceID: request.ClientDeviceID, DeviceName: request.ClientDeviceName, IdentityPublicKey: bytes.Clone(request.ClientIdentityPublicKey), PairedAt: m.config.Now(), LastSeenAt: m.config.Now()}
	if err := m.config.Store.SaveTrustedDevice(device); err != nil {
		return PairingResult{}, err
	}
	serverIdentityPublicKey := bytes.Clone(m.pending.ServerIdentity.Public)
	serverEphemeralPublicKey := bytes.Clone(m.pending.ServerEphemeral.Public)
	m.pending = nil
	return PairingResult{
		PairingID:                request.PairingID,
		SessionID:                bytes.Clone(sessionID),
		ClientDeviceID:           request.ClientDeviceID,
		ClientDeviceName:         request.ClientDeviceName,
		ServerDeviceID:           m.config.ServerDeviceID,
		ServerDeviceName:         m.config.ServerDeviceName,
		ServerIdentityPublicKey:  serverIdentityPublicKey,
		ServerEphemeralPublicKey: serverEphemeralPublicKey,
		Confirmation:             expectedConfirmation,
		ServerSession:            serverSession,
		Device:                   device,
	}, nil
}

func (m *Manager) Resume(request ResumeRequest) (ResumeResult, error) {
	trustedDevice, err := m.config.Store.LoadTrustedDevice(request.ClientDeviceID)
	if err != nil {
		return ResumeResult{}, ErrUnknownDevice
	}
	if !bytes.Equal(trustedDevice.IdentityPublicKey, request.ClientIdentityPublicKey) {
		return ResumeResult{}, ErrUnknownDevice
	}
	localIdentity, err := loadOrCreateLocalIdentity(m.config.Store, m.config.Rand)
	if err != nil {
		return ResumeResult{}, err
	}
	serverEphemeral, err := intcrypto.GenerateX25519KeyPair(m.config.Rand)
	if err != nil {
		return ResumeResult{}, err
	}
	transcript := resumeTranscript(
		request.PairingID,
		request.ClientDeviceID,
		request.ClientDeviceName,
		m.config.ServerDeviceID,
		m.config.ServerDeviceName,
		request.ClientIdentityPublicKey,
		request.ClientEphemeralPublicKey,
		localIdentity.Public,
		serverEphemeral.Public,
	)
	ee, err := intcrypto.ComputeSharedSecret(serverEphemeral.Private, request.ClientEphemeralPublicKey)
	if err != nil {
		return ResumeResult{}, err
	}
	ss, err := intcrypto.ComputeSharedSecret(localIdentity.Private, request.ClientIdentityPublicKey)
	if err != nil {
		return ResumeResult{}, err
	}
	confirmation, err := intcrypto.ComputeResumeConfirmation([][]byte{ee, ss}, transcript)
	if err != nil {
		return ResumeResult{}, err
	}
	sessionID, err := intcrypto.RandomSessionID(m.config.Rand)
	if err != nil {
		return ResumeResult{}, err
	}
	keys, err := intcrypto.DeriveSessionKeys([][]byte{ee, ss}, sessionID, transcript)
	if err != nil {
		return ResumeResult{}, err
	}
	serverSession, err := session.NewStateMachine(session.Config{SessionID: sessionID, SendKey: keys.ServerToClientKey, ReceiveKey: keys.ClientToServerKey, InactivityTimeout: m.config.InactivityTimeout, Now: m.config.Now, Rand: m.config.Rand})
	if err != nil {
		return ResumeResult{}, err
	}
	trustedDevice.LastSeenAt = m.config.Now()
	if err := m.config.Store.SaveTrustedDevice(trustedDevice); err != nil {
		return ResumeResult{}, err
	}
	return ResumeResult{PairingID: request.PairingID, SessionID: bytes.Clone(sessionID), ClientDeviceID: request.ClientDeviceID, ClientDeviceName: request.ClientDeviceName, ServerDeviceID: m.config.ServerDeviceID, ServerDeviceName: m.config.ServerDeviceName, ServerIdentityPublicKey: bytes.Clone(localIdentity.Public), ServerEphemeralPublicKey: bytes.Clone(serverEphemeral.Public), Confirmation: confirmation, ServerSession: serverSession, Device: trustedDevice}, nil
}

func (m *Manager) ForgetDevice(deviceID string) error {
	return m.config.Store.DeleteTrustedDevice(deviceID)
}

func randomPIN(reader io.Reader) (string, error) {
	raw := make([]byte, 6)
	oneByte := make([]byte, 1)
	for index := range raw {
		for {
			if _, err := io.ReadFull(reader, oneByte); err != nil {
				return "", err
			}
			if oneByte[0] >= 250 {
				continue
			}
			raw[index] = '0' + (oneByte[0] % 10)
			break
		}
	}
	return string(raw), nil
}

func randomHex(reader io.Reader, size int) (string, error) {
	raw := make([]byte, size)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func pairingTranscript(pairingID, clientDeviceID, clientDeviceName, serverDeviceID, serverDeviceName string, clientIdentityPublicKey, clientEphemeralPublicKey, serverIdentityPublicKey, serverEphemeralPublicKey []byte) intcrypto.Transcript {
	return intcrypto.Transcript{
		Version:                  "v1alpha1",
		Flow:                     intcrypto.FlowPairing,
		PairingID:                pairingID,
		ClientDeviceID:           clientDeviceID,
		ClientDeviceName:         clientDeviceName,
		ServerDeviceID:           serverDeviceID,
		ServerDeviceName:         serverDeviceName,
		ClientEphemeralPublicKey: clientEphemeralPublicKey,
		ServerEphemeralPublicKey: serverEphemeralPublicKey,
		ClientIdentityPublicKey:  clientIdentityPublicKey,
		ServerIdentityPublicKey:  serverIdentityPublicKey,
	}
}

func resumeTranscript(pairingID, clientDeviceID, clientDeviceName, serverDeviceID, serverDeviceName string, clientIdentityPublicKey, clientEphemeralPublicKey, serverIdentityPublicKey, serverEphemeralPublicKey []byte) intcrypto.Transcript {
	return intcrypto.Transcript{
		Version:                  "v1alpha1",
		Flow:                     intcrypto.FlowResume,
		PairingID:                pairingID,
		ClientDeviceID:           clientDeviceID,
		ClientDeviceName:         clientDeviceName,
		ServerDeviceID:           serverDeviceID,
		ServerDeviceName:         serverDeviceName,
		ClientEphemeralPublicKey: clientEphemeralPublicKey,
		ServerEphemeralPublicKey: serverEphemeralPublicKey,
		ClientIdentityPublicKey:  clientIdentityPublicKey,
		ServerIdentityPublicKey:  serverIdentityPublicKey,
	}
}
