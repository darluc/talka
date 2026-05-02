package pairing

import (
	"bytes"
	"crypto/ecdh"
	"errors"
	"fmt"
	"time"

	intcrypto "talka/internal/crypto"
)

type Store interface {
	LoadLocalIdentity() (LocalIdentity, error)
	SaveLocalIdentity(LocalIdentity) error
	LoadTrustedDevice(deviceID string) (TrustedDevice, error)
	SaveTrustedDevice(TrustedDevice) error
	DeleteTrustedDevice(deviceID string) error
}

type LocalIdentity struct {
	PrivateKey []byte `json:"private_key"`
	PublicKey  []byte `json:"public_key"`
}

type TrustedDevice struct {
	DeviceID          string    `json:"device_id"`
	DeviceName        string    `json:"device_name"`
	IdentityPublicKey []byte    `json:"identity_public_key"`
	PairedAt          time.Time `json:"paired_at"`
	LastSeenAt        time.Time `json:"last_seen_at"`
}

func loadOrCreateLocalIdentity(store Store, reader ioReader) (intcrypto.KeyPair, error) {
	identity, err := store.LoadLocalIdentity()
	if err == nil {
		privateKey, privateErr := ecdh.X25519().NewPrivateKey(identity.PrivateKey)
		if privateErr != nil {
			return intcrypto.KeyPair{}, privateErr
		}
		return intcrypto.KeyPair{Private: privateKey, Public: bytes.Clone(identity.PublicKey)}, nil
	}
	if !errors.Is(err, ErrLocalIdentityNotFound) {
		return intcrypto.KeyPair{}, err
	}

	generated, err := intcrypto.GenerateX25519KeyPair(reader)
	if err != nil {
		return intcrypto.KeyPair{}, err
	}
	if err := store.SaveLocalIdentity(LocalIdentity{PrivateKey: generated.Private.Bytes(), PublicKey: generated.Public}); err != nil {
		return intcrypto.KeyPair{}, err
	}
	return generated, nil
}

type ioReader interface {
	Read(p []byte) (n int, err error)
}

func cloneTrustedDevice(device TrustedDevice) TrustedDevice {
	device.IdentityPublicKey = bytes.Clone(device.IdentityPublicKey)
	return device
}

func cloneLocalIdentity(identity LocalIdentity) LocalIdentity {
	return LocalIdentity{PrivateKey: bytes.Clone(identity.PrivateKey), PublicKey: bytes.Clone(identity.PublicKey)}
}

func validatePublicKey(publicKey []byte) error {
	if len(publicKey) == 0 {
		return fmt.Errorf("public key is required")
	}
	_, err := ecdh.X25519().NewPublicKey(publicKey)
	return err
}
