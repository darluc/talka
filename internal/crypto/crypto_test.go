package crypto

import (
	"bytes"
	crand "crypto/rand"
	"testing"
)

func TestPairingConfirmationMatchesOnBothSides(t *testing.T) {
	clientEphemeral, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}
	serverEphemeral, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(server ephemeral) error = %v", err)
	}
	clientIdentity, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	serverIdentity, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(server identity) error = %v", err)
	}

	clientEE, err := ComputeSharedSecret(clientEphemeral.Private, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(client ee) error = %v", err)
	}
	serverEE, err := ComputeSharedSecret(serverEphemeral.Private, clientEphemeral.Public)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(server ee) error = %v", err)
	}
	if !bytes.Equal(clientEE, serverEE) {
		t.Fatal("ephemeral shared secrets do not match")
	}

	clientSS, err := ComputeSharedSecret(clientIdentity.Private, serverIdentity.Public)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(client ss) error = %v", err)
	}
	serverSS, err := ComputeSharedSecret(serverIdentity.Private, clientIdentity.Public)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(server ss) error = %v", err)
	}
	if !bytes.Equal(clientSS, serverSS) {
		t.Fatal("identity shared secrets do not match")
	}

	transcript := Transcript{
		Version:                  "v1alpha1",
		Flow:                     FlowPairing,
		PairingID:                "pair-123",
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ServerDeviceID:           "mac-device",
		ServerDeviceName:         "Alice Mac",
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ServerEphemeralPublicKey: serverEphemeral.Public,
		ClientIdentityPublicKey:  clientIdentity.Public,
		ServerIdentityPublicKey:  serverIdentity.Public,
	}

	clientConfirmation, err := ComputePairingConfirmation([][]byte{clientEE, clientSS}, "123456", transcript)
	if err != nil {
		t.Fatalf("ComputePairingConfirmation(client) error = %v", err)
	}
	serverConfirmation, err := ComputePairingConfirmation([][]byte{serverEE, serverSS}, "123456", transcript)
	if err != nil {
		t.Fatalf("ComputePairingConfirmation(server) error = %v", err)
	}

	if !bytes.Equal(clientConfirmation, serverConfirmation) {
		t.Fatal("pairing confirmations do not match")
	}
}

func TestDeriveSessionKeysChangesAcrossReconnects(t *testing.T) {
	clientIdentity, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client identity) error = %v", err)
	}
	serverIdentity, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(server identity) error = %v", err)
	}

	firstKeys := deriveKeysForReconnect(t, clientIdentity, serverIdentity)
	secondKeys := deriveKeysForReconnect(t, clientIdentity, serverIdentity)

	if bytes.Equal(firstKeys.ClientToServerKey, secondKeys.ClientToServerKey) {
		t.Fatal("ClientToServerKey should change across reconnects")
	}
	if bytes.Equal(firstKeys.ServerToClientKey, secondKeys.ServerToClientKey) {
		t.Fatal("ServerToClientKey should change across reconnects")
	}
}

func deriveKeysForReconnect(t *testing.T, clientIdentity, serverIdentity KeyPair) SessionKeys {
	t.Helper()

	clientEphemeral, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(client ephemeral) error = %v", err)
	}
	serverEphemeral, err := GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(server ephemeral) error = %v", err)
	}

	ee, err := ComputeSharedSecret(clientEphemeral.Private, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ee) error = %v", err)
	}
	ss, err := ComputeSharedSecret(clientIdentity.Private, serverIdentity.Public)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ss) error = %v", err)
	}

	transcript := Transcript{
		Version:                  "v1alpha1",
		Flow:                     FlowResume,
		PairingID:                "pair-123",
		ClientDeviceID:           "ios-device",
		ClientDeviceName:         "Alice iPhone",
		ServerDeviceID:           "mac-device",
		ServerDeviceName:         "Alice Mac",
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ServerEphemeralPublicKey: serverEphemeral.Public,
		ClientIdentityPublicKey:  clientIdentity.Public,
		ServerIdentityPublicKey:  serverIdentity.Public,
	}

	sessionID, err := RandomSessionID(crand.Reader)
	if err != nil {
		t.Fatalf("RandomSessionID() error = %v", err)
	}

	keys, err := DeriveSessionKeys([][]byte{ee, ss}, sessionID, transcript)
	if err != nil {
		t.Fatalf("DeriveSessionKeys() error = %v", err)
	}
	return keys
}
