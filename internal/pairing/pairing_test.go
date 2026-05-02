package pairing

import (
	"bytes"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	intcrypto "talka/internal/crypto"
	"talka/internal/protocol"
	"talka/internal/session"
)

func TestManagerAcceptsCorrectPINAndStoresTrustedDevice(t *testing.T) {
	clock := newFakeClock(time.Unix(100, 0))
	store := NewFakeStore()
	manager := NewManager(Config{
		ServerDeviceID:    "mac-device",
		ServerDeviceName:  "Alice Mac",
		Store:             store,
		Now:               clock.Now,
		Rand:              crand.Reader,
		PairingTTL:        2 * time.Minute,
		InactivityTimeout: 30 * time.Second,
		MaxFailures:       3,
	})

	start, err := manager.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(start.ServerEphemeralPublicKey) == 0 {
		t.Fatal("ServerEphemeralPublicKey is empty")
	}
	if len(start.ServerIdentityPublicKey) == 0 {
		t.Fatal("ServerIdentityPublicKey is empty")
	}

	clientIdentity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}

	result, err := manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", start.PIN),
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	trusted, err := store.LoadTrustedDevice("ios-device")
	if err != nil {
		t.Fatalf("LoadTrustedDevice() error = %v", err)
	}
	if trusted.DeviceName != "Alice iPhone" {
		t.Fatalf("DeviceName = %q, want %q", trusted.DeviceName, "Alice iPhone")
	}
	if got := result.ServerSession.State(); got != "active" {
		t.Fatalf("ServerSession.State() = %q, want active", got)
	}

	clientSession, err := newClientSession(start, result, clientIdentity, clientEphemeral, 30*time.Second, clock)
	if err != nil {
		t.Fatalf("newClientSession() error = %v", err)
	}

	msg, err := clientSession.Encrypt(protocol.MessageTypePing, []byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := result.ServerSession.Decrypt(msg)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(plaintext, []byte("hello")) {
		t.Fatalf("plaintext = %q, want %q", plaintext, []byte("hello"))
	}
}

func TestManagerRejectsWrongPIN(t *testing.T) {
	manager, _, _ := newTestManager(t)
	start, err := manager.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clientIdentity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}

	_, err = manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", "000000"),
	})
	if !errors.Is(err, ErrInvalidPIN) {
		t.Fatalf("Complete() error = %v, want %v", err, ErrInvalidPIN)
	}
}

func TestManagerRejectsExpiredPIN(t *testing.T) {
	manager, clock, _ := newTestManager(t)
	start, err := manager.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clock.Advance(2*time.Minute + time.Second)

	clientIdentity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}

	_, err = manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", start.PIN),
	})
	if !errors.Is(err, ErrExpiredPIN) {
		t.Fatalf("Complete() error = %v, want %v", err, ErrExpiredPIN)
	}
}

func TestManagerRateLimitsRepeatedFailures(t *testing.T) {
	manager, _, _ := newTestManager(t)
	start, err := manager.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clientIdentity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}

	for attempt := range 2 {
		_, err = manager.Complete(PairingRequest{
			PairingID:                start.PairingID,
			ClientDeviceID:           "ios-device",
			ClientDeviceName:         "Alice iPhone",
			ClientIdentityPublicKey:  clientIdentity.Public,
			ClientEphemeralPublicKey: clientEphemeral.Public,
			ClientConfirmation:       mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", "000000"),
		})
		if !errors.Is(err, ErrInvalidPIN) {
			t.Fatalf("attempt %d error = %v, want %v", attempt+1, err, ErrInvalidPIN)
		}
	}

	_, err = manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", "000000"),
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third attempt error = %v, want %v", err, ErrRateLimited)
	}
}

func TestManagerReconnectsWithFreshSessionKeys(t *testing.T) {
	manager, clock, _ := newTestManager(t)
	start, initial, clientIdentity, _, firstClientSession := pairClient(t, manager, clock)

	reconnectedClientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}

	resume, err := manager.Resume(ResumeRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: reconnectedClientEphemeral.Public,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	clientSession, err := newResumedClientSession(start, resume, clientIdentity, reconnectedClientEphemeral, 30*time.Second, clock)
	if err != nil {
		t.Fatalf("newResumedClientSession() error = %v", err)
	}

	if bytes.Equal(firstClientSession.SendKey(), clientSession.SendKey()) {
		t.Fatal("SendKey should change across reconnects")
	}
	if bytes.Equal(initial.ServerSession.ReceiveKey(), resume.ServerSession.ReceiveKey()) {
		t.Fatal("server receive key should change across reconnects")
	}
}

func TestManagerRejectsForgottenDevice(t *testing.T) {
	manager, clock, _ := newTestManager(t)
	start, _, clientIdentity, _, _ := pairClient(t, manager, clock)

	if err := manager.ForgetDevice("ios-device"); err != nil {
		t.Fatalf("ForgetDevice() error = %v", err)
	}

	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}

	_, err = manager.Resume(ResumeRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
	})
	if !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("Resume() error = %v, want %v", err, ErrUnknownDevice)
	}
}

func TestFakeStoreRoundTripsLocalIdentity(t *testing.T) {
	store := NewFakeStore()
	identity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair() error = %v", err)
	}

	err = store.SaveLocalIdentity(LocalIdentity{PrivateKey: identity.Private.Bytes(), PublicKey: identity.Public})
	if err != nil {
		t.Fatalf("SaveLocalIdentity() error = %v", err)
	}

	loaded, err := store.LoadLocalIdentity()
	if err != nil {
		t.Fatalf("LoadLocalIdentity() error = %v", err)
	}
	if !bytes.Equal(loaded.PublicKey, identity.Public) {
		t.Fatal("loaded public key mismatch")
	}
}

func TestLoadOrCreateLocalIdentityCreatesOnlyOnNotFound(t *testing.T) {
	store := NewFakeStore()
	identity, err := loadOrCreateLocalIdentity(store, crand.Reader)
	if err != nil {
		t.Fatalf("loadOrCreateLocalIdentity() error = %v", err)
	}
	loaded, err := store.LoadLocalIdentity()
	if err != nil {
		t.Fatalf("LoadLocalIdentity() error = %v", err)
	}
	if !bytes.Equal(loaded.PublicKey, identity.Public) {
		t.Fatal("stored public key mismatch")
	}
}

func TestLoadOrCreateLocalIdentityPropagatesStoreErrors(t *testing.T) {
	wantErr := errors.New("permission denied")
	_, err := loadOrCreateLocalIdentity(errorStore{err: wantErr}, crand.Reader)
	if !errors.Is(err, wantErr) {
		t.Fatalf("loadOrCreateLocalIdentity() error = %v, want %v", err, wantErr)
	}
}

func TestRandomPINUsesUnbiasedDigits(t *testing.T) {
	reader := &scriptedReader{chunks: [][]byte{{250, 251, 252, 253, 254, 255}, {0, 1, 2, 3, 4, 5}}}
	pin, err := randomPIN(reader)
	if err != nil {
		t.Fatalf("randomPIN() error = %v", err)
	}
	if pin != "012345" {
		t.Fatalf("randomPIN() = %q, want %q", pin, "012345")
	}
}

func TestManagerUsesClientConfirmationInsteadOfPlaintextPIN(t *testing.T) {
	manager, _, _ := newTestManager(t)
	start, err := manager.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clientIdentity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}
	goodConfirmation := mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", start.PIN)
	badConfirmation := bytes.Clone(goodConfirmation)
	badConfirmation[len(badConfirmation)-1] ^= 0x01

	_, err = manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       badConfirmation,
	})
	if !errors.Is(err, ErrInvalidPIN) {
		t.Fatalf("Complete() error = %v, want %v", err, ErrInvalidPIN)
	}

	result, err := manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       goodConfirmation,
	})
	if err != nil {
		t.Fatalf("Complete(correct confirmation) error = %v", err)
	}
	if len(result.Confirmation) == 0 {
		t.Fatal("server confirmation is empty")
	}
}

func TestManagerSupportsConcurrentStartAndComplete(t *testing.T) {
	manager, _, _ := newTestManager(t)
	if _, err := manager.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, _ = manager.Start()
			}
		}()
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, _ = manager.Complete(PairingRequest{PairingID: "missing"})
			}
		}()
	}
	wg.Wait()
}

func newTestManager(t *testing.T) (*Manager, *fakeClock, *FakeStore) {
	t.Helper()

	clock := newFakeClock(time.Unix(100, 0))
	store := NewFakeStore()
	manager := NewManager(Config{
		ServerDeviceID:    "mac-device",
		ServerDeviceName:  "Alice Mac",
		Store:             store,
		Now:               clock.Now,
		Rand:              crand.Reader,
		PairingTTL:        2 * time.Minute,
		InactivityTimeout: 30 * time.Second,
		MaxFailures:       3,
	})
	return manager, clock, store
}

func pairClient(t *testing.T, manager *Manager, clock *fakeClock) (StartResponse, PairingResult, intcrypto.KeyPair, intcrypto.KeyPair, ClientSessionView) {
	t.Helper()

	start, err := manager.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clientIdentity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}
	result, err := manager.Complete(PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       mustClientPairingConfirmation(t, start, "Alice Mac", clientIdentity, clientEphemeral, "ios-device", "Alice iPhone", start.PIN),
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	clientSession, err := newClientSession(start, result, clientIdentity, clientEphemeral, 30*time.Second, clock)
	if err != nil {
		t.Fatalf("newClientSession() error = %v", err)
	}
	return start, result, clientIdentity, clientEphemeral, clientSession
}

type timeSource interface {
	Now() time.Time
}

type ClientSessionView interface {
	Encrypt(protocol.MessageType, []byte) (session.EncryptedMessage, error)
	SendKey() []byte
}

func newClientSession(start StartResponse, result PairingResult, clientIdentity, clientEphemeral intcrypto.KeyPair, timeout time.Duration, clock timeSource) (ClientSessionView, error) {
	transcript := pairingTranscript(
		start.PairingID,
		result.ClientDeviceID,
		result.ClientDeviceName,
		result.ServerDeviceID,
		result.ServerDeviceName,
		clientIdentity.Public,
		clientEphemeral.Public,
		start.ServerIdentityPublicKey,
		start.ServerEphemeralPublicKey,
	)
	ee, err := intcrypto.ComputeSharedSecret(clientEphemeral.Private, start.ServerEphemeralPublicKey)
	if err != nil {
		return nil, err
	}
	ss, err := intcrypto.ComputeSharedSecret(clientIdentity.Private, start.ServerIdentityPublicKey)
	if err != nil {
		return nil, err
	}
	confirmation, err := intcrypto.ComputePairingConfirmation([][]byte{ee, ss}, start.PIN, transcript)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(confirmation, result.Confirmation) {
		return nil, fmt.Errorf("pairing confirmation mismatch")
	}
	keys, err := intcrypto.DeriveSessionKeys([][]byte{ee, ss}, result.SessionID, transcript)
	if err != nil {
		return nil, err
	}
	return session.NewStateMachine(session.Config{SessionID: result.SessionID, SendKey: keys.ClientToServerKey, ReceiveKey: keys.ServerToClientKey, InactivityTimeout: timeout, Now: clock.Now})
}

func newResumedClientSession(start StartResponse, result ResumeResult, clientIdentity, clientEphemeral intcrypto.KeyPair, timeout time.Duration, clock timeSource) (ClientSessionView, error) {
	transcript := resumeTranscript(
		start.PairingID,
		result.ClientDeviceID,
		result.ClientDeviceName,
		result.ServerDeviceID,
		result.ServerDeviceName,
		clientIdentity.Public,
		clientEphemeral.Public,
		result.ServerIdentityPublicKey,
		result.ServerEphemeralPublicKey,
	)
	ee, err := intcrypto.ComputeSharedSecret(clientEphemeral.Private, result.ServerEphemeralPublicKey)
	if err != nil {
		return nil, err
	}
	ss, err := intcrypto.ComputeSharedSecret(clientIdentity.Private, result.ServerIdentityPublicKey)
	if err != nil {
		return nil, err
	}
	confirmation, err := intcrypto.ComputeResumeConfirmation([][]byte{ee, ss}, transcript)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(confirmation, result.Confirmation) {
		return nil, fmt.Errorf("resume confirmation mismatch")
	}
	keys, err := intcrypto.DeriveSessionKeys([][]byte{ee, ss}, result.SessionID, transcript)
	if err != nil {
		return nil, err
	}
	return session.NewStateMachine(session.Config{SessionID: result.SessionID, SendKey: keys.ClientToServerKey, ReceiveKey: keys.ServerToClientKey, InactivityTimeout: timeout, Now: clock.Now})
}

type fakeClock struct {
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

type errorStore struct {
	err error
}

func (s errorStore) LoadLocalIdentity() (LocalIdentity, error) {
	return LocalIdentity{}, s.err
}

func (s errorStore) SaveLocalIdentity(LocalIdentity) error {
	return nil
}

func (s errorStore) LoadTrustedDevice(string) (TrustedDevice, error) {
	return TrustedDevice{}, ErrUnknownDevice
}

func (s errorStore) SaveTrustedDevice(TrustedDevice) error {
	return nil
}

func (s errorStore) DeleteTrustedDevice(string) error {
	return nil
}

type scriptedReader struct {
	chunks [][]byte
	index  int
	inner  int
}

func (r *scriptedReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	n := copy(p, chunk[r.inner:])
	r.inner += n
	if r.inner >= len(chunk) {
		r.index++
		r.inner = 0
	}
	return n, nil
}

func mustClientPairingConfirmation(t *testing.T, start StartResponse, serverDeviceName string, clientIdentity, clientEphemeral intcrypto.KeyPair, clientDeviceID, clientDeviceName, pin string) []byte {
	t.Helper()
	transcript := intcrypto.Transcript{
		Version:                  "v1alpha1",
		Flow:                     intcrypto.FlowPairing,
		PairingID:                start.PairingID,
		ClientDeviceID:           clientDeviceID,
		ClientDeviceName:         clientDeviceName,
		ServerDeviceID:           "mac-device",
		ServerDeviceName:         serverDeviceName,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ServerEphemeralPublicKey: start.ServerEphemeralPublicKey,
		ClientIdentityPublicKey:  clientIdentity.Public,
		ServerIdentityPublicKey:  start.ServerIdentityPublicKey,
	}
	ee, err := intcrypto.ComputeSharedSecret(clientEphemeral.Private, start.ServerEphemeralPublicKey)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ee) error = %v", err)
	}
	ss, err := intcrypto.ComputeSharedSecret(clientIdentity.Private, start.ServerIdentityPublicKey)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ss) error = %v", err)
	}
	confirmation, err := intcrypto.ComputePairingConfirmation([][]byte{ee, ss}, pin, transcript)
	if err != nil {
		t.Fatalf("ComputePairingConfirmation() error = %v", err)
	}
	return confirmation
}
