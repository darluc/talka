package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	intcrypto "talka/internal/crypto"
	"talka/internal/pairing"
	"talka/internal/protocol"
	"talka/internal/session"
)

const (
	serverDeviceID   = "mac-device"
	serverDeviceName = "Smoke Mac"
	clientDeviceID   = "ios-device"
	clientDeviceName = "Smoke iPhone"
)

func main() {
	mode := flag.String("pin", "correct", "correct or wrong")
	replay := flag.Bool("replay", false, "exercise replay rejection")
	flag.Parse()

	clock := &wallClock{}
	manager := pairing.NewManager(pairing.Config{ServerDeviceID: serverDeviceID, ServerDeviceName: serverDeviceName, Store: pairing.NewFakeStore(), Now: clock.Now, Rand: rand.Reader, PairingTTL: 2 * time.Minute, InactivityTimeout: 30 * time.Second, MaxFailures: 3})

	if *mode != "correct" {
		if err := runWrongConfirmationScenario(manager); err != nil {
			fatal(err)
		}
		fmt.Println("EXPECTED_ERROR invalid_pin")
		if *replay {
			if err := runReplayScenario(manager, clock); err != nil {
				fatal(err)
			}
			fmt.Println("EXPECTED_ERROR replay")
		}
		return
	}

	if err := runSuccessScenario(manager, clock); err != nil {
		fatal(err)
	}
	if *replay {
		if err := runReplayScenario(manager, clock); err != nil {
			fatal(err)
		}
		fmt.Println("EXPECTED_ERROR replay")
	}
}

func runWrongConfirmationScenario(manager *pairing.Manager) error {
	start, clientIdentity, clientEphemeral, err := startClientPairing(manager)
	if err != nil {
		return err
	}
	request, err := newPairingRequest(start, clientIdentity, clientEphemeral, clientDeviceID, clientDeviceName, serverDeviceID, serverDeviceName, "000000")
	if err != nil {
		return err
	}
	_, err = manager.Complete(request)
	if !errors.Is(err, pairing.ErrInvalidPIN) && !errors.Is(err, pairing.ErrRateLimited) {
		return fmt.Errorf("expected invalid pin error, got %v", err)
	}
	return nil
}

func runSuccessScenario(manager *pairing.Manager, clock clockSource) error {
	start, clientIdentity, clientEphemeral, err := startClientPairing(manager)
	if err != nil {
		return err
	}
	request, err := newPairingRequest(start, clientIdentity, clientEphemeral, clientDeviceID, clientDeviceName, serverDeviceID, serverDeviceName, start.PIN)
	if err != nil {
		return err
	}
	result, err := manager.Complete(request)
	if err != nil {
		return err
	}
	clientSession, err := newClientSession(start, result, clientIdentity, clientEphemeral, 30*time.Second, clock)
	if err != nil {
		return err
	}
	message, err := clientSession.Encrypt(protocol.MessageTypePing, []byte("smoke"))
	if err != nil {
		return err
	}
	plaintext, err := result.ServerSession.Decrypt(message)
	if err != nil {
		return err
	}
	fmt.Printf("PAIRING_OK %s\n", start.PairingID)
	fmt.Printf("SESSION_OK %s\n", plaintext)

	resumedClientEphemeral, err := intcrypto.GenerateX25519KeyPair(rand.Reader)
	if err != nil {
		return err
	}
	resume, err := manager.Resume(pairing.ResumeRequest{PairingID: start.PairingID, ClientDeviceID: clientDeviceID, ClientDeviceName: clientDeviceName, ClientIdentityPublicKey: clientIdentity.Public, ClientEphemeralPublicKey: resumedClientEphemeral.Public})
	if err != nil {
		return err
	}
	resumedClientSession, err := newResumedClientSession(start, resume, clientIdentity, resumedClientEphemeral, 30*time.Second, clock)
	if err != nil {
		return err
	}
	if string(resumedClientSession.SendKey()) == string(clientSession.SendKey()) {
		return fmt.Errorf("expected fresh session keys on reconnect")
	}
	fmt.Println("RECONNECT_OK")
	return nil
}

func runReplayScenario(manager *pairing.Manager, clock clockSource) error {
	start, clientIdentity, clientEphemeral, err := startClientPairing(manager)
	if err != nil {
		return err
	}
	request, err := newPairingRequest(start, clientIdentity, clientEphemeral, clientDeviceID, clientDeviceName, serverDeviceID, serverDeviceName, start.PIN)
	if err != nil {
		return err
	}
	result, err := manager.Complete(request)
	if err != nil {
		return err
	}
	clientSession, err := newClientSession(start, result, clientIdentity, clientEphemeral, 30*time.Second, clock)
	if err != nil {
		return err
	}
	message, err := clientSession.Encrypt(protocol.MessageTypePing, []byte("smoke"))
	if err != nil {
		return err
	}
	if _, err := result.ServerSession.Decrypt(message); err != nil {
		return err
	}
	_, err = result.ServerSession.Decrypt(message)
	if !errors.Is(err, session.ErrReplay) {
		return fmt.Errorf("expected replay error, got %v", err)
	}
	return nil
}

func startClientPairing(manager *pairing.Manager) (pairing.StartResponse, intcrypto.KeyPair, intcrypto.KeyPair, error) {
	start, err := manager.Start()
	if err != nil {
		return pairing.StartResponse{}, intcrypto.KeyPair{}, intcrypto.KeyPair{}, err
	}
	clientIdentity, err := intcrypto.GenerateX25519KeyPair(rand.Reader)
	if err != nil {
		return pairing.StartResponse{}, intcrypto.KeyPair{}, intcrypto.KeyPair{}, err
	}
	clientEphemeral, err := intcrypto.GenerateX25519KeyPair(rand.Reader)
	if err != nil {
		return pairing.StartResponse{}, intcrypto.KeyPair{}, intcrypto.KeyPair{}, err
	}
	return start, clientIdentity, clientEphemeral, nil
}

type wallClock struct{}

func (w *wallClock) Now() time.Time {
	return time.Now()
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
