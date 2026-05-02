package main

import (
	"bytes"
	"fmt"
	"time"

	intcrypto "talka/internal/crypto"
	"talka/internal/pairing"
	"talka/internal/session"
)

type clockSource interface {
	Now() time.Time
}

func newPairingRequest(start pairing.StartResponse, clientIdentity, clientEphemeral intcrypto.KeyPair, clientDeviceID, clientDeviceName, serverDeviceID, serverDeviceName, pin string) (pairing.PairingRequest, error) {
	transcript := intcrypto.Transcript{
		Version:                  "v1alpha1",
		Flow:                     intcrypto.FlowPairing,
		PairingID:                start.PairingID,
		ClientDeviceID:           clientDeviceID,
		ClientDeviceName:         clientDeviceName,
		ServerDeviceID:           serverDeviceID,
		ServerDeviceName:         serverDeviceName,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ServerEphemeralPublicKey: start.ServerEphemeralPublicKey,
		ClientIdentityPublicKey:  clientIdentity.Public,
		ServerIdentityPublicKey:  start.ServerIdentityPublicKey,
	}
	ee, err := intcrypto.ComputeSharedSecret(clientEphemeral.Private, start.ServerEphemeralPublicKey)
	if err != nil {
		return pairing.PairingRequest{}, err
	}
	ss, err := intcrypto.ComputeSharedSecret(clientIdentity.Private, start.ServerIdentityPublicKey)
	if err != nil {
		return pairing.PairingRequest{}, err
	}
	confirmation, err := intcrypto.ComputePairingConfirmation([][]byte{ee, ss}, pin, transcript)
	if err != nil {
		return pairing.PairingRequest{}, err
	}
	return pairing.PairingRequest{
		PairingID:                start.PairingID,
		ClientDeviceID:           clientDeviceID,
		ClientDeviceName:         clientDeviceName,
		ClientIdentityPublicKey:  clientIdentity.Public,
		ClientEphemeralPublicKey: clientEphemeral.Public,
		ClientConfirmation:       confirmation,
	}, nil
}

func newClientSession(start pairing.StartResponse, result pairing.PairingResult, clientIdentity, clientEphemeral intcrypto.KeyPair, timeout time.Duration, clock clockSource) (*session.StateMachine, error) {
	transcript := intcrypto.Transcript{Version: "v1alpha1", Flow: intcrypto.FlowPairing, PairingID: start.PairingID, ClientDeviceID: result.ClientDeviceID, ClientDeviceName: result.ClientDeviceName, ServerDeviceID: result.ServerDeviceID, ServerDeviceName: result.ServerDeviceName, ClientEphemeralPublicKey: clientEphemeral.Public, ServerEphemeralPublicKey: start.ServerEphemeralPublicKey, ClientIdentityPublicKey: clientIdentity.Public, ServerIdentityPublicKey: start.ServerIdentityPublicKey}
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

func newResumedClientSession(start pairing.StartResponse, result pairing.ResumeResult, clientIdentity, clientEphemeral intcrypto.KeyPair, timeout time.Duration, clock clockSource) (*session.StateMachine, error) {
	transcript := intcrypto.Transcript{Version: "v1alpha1", Flow: intcrypto.FlowResume, PairingID: start.PairingID, ClientDeviceID: result.ClientDeviceID, ClientDeviceName: result.ClientDeviceName, ServerDeviceID: result.ServerDeviceID, ServerDeviceName: result.ServerDeviceName, ClientEphemeralPublicKey: clientEphemeral.Public, ServerEphemeralPublicKey: result.ServerEphemeralPublicKey, ClientIdentityPublicKey: clientIdentity.Public, ServerIdentityPublicKey: result.ServerIdentityPublicKey}
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
