package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	sessionIDSize                = 32
	pairingConfirmationKeyInfo   = "talka/pairing-confirmation-key/v1"
	resumeConfirmationKeyInfo    = "talka/resume-confirmation-key/v1"
	sessionKeyDerivationInfo     = "talka/session-keys/v1"
	sessionKeyBytes              = 32
	totalSessionKeyMaterialBytes = 64
)

type Flow string

const (
	FlowPairing Flow = "pairing"
	FlowResume  Flow = "resume"
)

type KeyPair struct {
	Private *ecdh.PrivateKey
	Public  []byte
}

type Transcript struct {
	Version                  string
	Flow                     Flow
	PairingID                string
	ClientDeviceID           string
	ClientDeviceName         string
	ServerDeviceID           string
	ServerDeviceName         string
	ClientEphemeralPublicKey []byte
	ServerEphemeralPublicKey []byte
	ClientIdentityPublicKey  []byte
	ServerIdentityPublicKey  []byte
}

type SessionKeys struct {
	ClientToServerKey []byte
	ServerToClientKey []byte
}

func GenerateX25519KeyPair(reader io.Reader) (KeyPair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Private: privateKey, Public: bytes.Clone(privateKey.PublicKey().Bytes())}, nil
}

func ComputeSharedSecret(privateKey *ecdh.PrivateKey, peerPublicKey []byte) ([]byte, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("private key is required")
	}
	publicKey, err := ecdh.X25519().NewPublicKey(peerPublicKey)
	if err != nil {
		return nil, err
	}
	secret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return nil, err
	}
	return bytes.Clone(secret), nil
}

func ComputePairingConfirmation(sharedSecrets [][]byte, pin string, transcript Transcript) ([]byte, error) {
	if pin == "" {
		return nil, fmt.Errorf("pin is required")
	}
	key, err := hkdfBytes(joinSecrets(sharedSecrets), []byte(pin), []byte(pairingConfirmationKeyInfo), sha256.Size)
	if err != nil {
		return nil, err
	}
	return computeTranscriptMAC(key, transcript), nil
}

func ComputeResumeConfirmation(sharedSecrets [][]byte, transcript Transcript) ([]byte, error) {
	key, err := hkdfBytes(joinSecrets(sharedSecrets), []byte(transcript.ClientDeviceID), []byte(resumeConfirmationKeyInfo), sha256.Size)
	if err != nil {
		return nil, err
	}
	return computeTranscriptMAC(key, transcript), nil
}

func DeriveSessionKeys(sharedSecrets [][]byte, sessionID []byte, transcript Transcript) (SessionKeys, error) {
	if len(sessionID) == 0 {
		return SessionKeys{}, fmt.Errorf("session id is required")
	}
	transcriptHash := sha256.Sum256(marshalTranscript(transcript))
	salt := append(bytes.Clone(sessionID), transcriptHash[:]...)
	material, err := hkdfBytes(joinSecrets(sharedSecrets), salt, []byte(sessionKeyDerivationInfo), totalSessionKeyMaterialBytes)
	if err != nil {
		return SessionKeys{}, err
	}
	return SessionKeys{
		ClientToServerKey: bytes.Clone(material[:sessionKeyBytes]),
		ServerToClientKey: bytes.Clone(material[sessionKeyBytes:]),
	}, nil
}

func RandomSessionID(reader io.Reader) ([]byte, error) {
	sessionID := make([]byte, sessionIDSize)
	if _, err := io.ReadFull(reader, sessionID); err != nil {
		return nil, err
	}
	return sessionID, nil
}

func computeTranscriptMAC(key []byte, transcript Transcript) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(marshalTranscript(transcript))
	return mac.Sum(nil)
}

func marshalTranscript(transcript Transcript) []byte {
	buffer := bytes.NewBuffer(nil)
	writeString(buffer, transcript.Version)
	writeString(buffer, string(transcript.Flow))
	writeString(buffer, transcript.PairingID)
	writeString(buffer, transcript.ClientDeviceID)
	writeString(buffer, transcript.ClientDeviceName)
	writeString(buffer, transcript.ServerDeviceID)
	writeString(buffer, transcript.ServerDeviceName)
	writeBytes(buffer, transcript.ClientEphemeralPublicKey)
	writeBytes(buffer, transcript.ServerEphemeralPublicKey)
	writeBytes(buffer, transcript.ClientIdentityPublicKey)
	writeBytes(buffer, transcript.ServerIdentityPublicKey)
	return buffer.Bytes()
}

func writeString(buffer *bytes.Buffer, value string) {
	writeBytes(buffer, []byte(value))
}

func writeBytes(buffer *bytes.Buffer, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = buffer.Write(length[:])
	_, _ = buffer.Write(value)
}

func hkdfBytes(secret, salt, info []byte, size int) ([]byte, error) {
	reader := hkdf.New(sha256.New, secret, salt, info)
	out := make([]byte, size)
	if _, err := io.ReadFull(reader, out); err != nil {
		return nil, err
	}
	return out, nil
}

func joinSecrets(secrets [][]byte) []byte {
	buffer := bytes.NewBuffer(nil)
	for _, secret := range secrets {
		writeBytes(buffer, secret)
	}
	return buffer.Bytes()
}
