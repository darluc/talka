package session

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"talka/internal/protocol"
)

const (
	Version   uint8 = 1
	NonceSize       = chacha20poly1305.NonceSize
	TagSize         = chacha20poly1305.Overhead
)

var (
	ErrReplay     = errors.New("replayed sequence")
	ErrOutOfOrder = errors.New("out of order sequence")
	ErrExpired    = errors.New("session expired")
	ErrForgotten  = errors.New("device trust invalidated")
)

type State string

const (
	StateActive    State = "active"
	StateExpired   State = "expired"
	StateForgotten State = "forgotten"
)

type Config struct {
	SessionID         []byte
	SendKey           []byte
	ReceiveKey        []byte
	InactivityTimeout time.Duration
	Now               func() time.Time
	Rand              io.Reader
}

type EncryptedMessage struct {
	Version    uint8
	SessionID  []byte
	Seq        uint64
	Type       protocol.MessageType
	Nonce      []byte
	Ciphertext []byte
	Tag        []byte
}

type StateMachine struct {
	mu                sync.Mutex
	sessionID         []byte
	sendKey           []byte
	receiveKey        []byte
	sendAEAD          cipherState
	receiveAEAD       cipherState
	inactivityTimeout time.Duration
	now               func() time.Time
	rand              io.Reader
	state             State
	lastActivity      time.Time
	nextLocalSeq      uint64
	lastRemoteSeq     uint64
	seenRemoteSeq     map[uint64]struct{}
}

type cipherState interface {
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
}

func NewStateMachine(config Config) (*StateMachine, error) {
	if len(config.SessionID) == 0 {
		return nil, fmt.Errorf("session id is required")
	}
	if len(config.SendKey) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("send key must be %d bytes", chacha20poly1305.KeySize)
	}
	if len(config.ReceiveKey) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("receive key must be %d bytes", chacha20poly1305.KeySize)
	}
	if config.InactivityTimeout <= 0 {
		return nil, fmt.Errorf("inactivity timeout must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Rand == nil {
		config.Rand = rand.Reader
	}

	sendAEAD, err := chacha20poly1305.New(config.SendKey)
	if err != nil {
		return nil, err
	}
	receiveAEAD, err := chacha20poly1305.New(config.ReceiveKey)
	if err != nil {
		return nil, err
	}
	now := config.Now()
	return &StateMachine{
		sessionID:         bytes.Clone(config.SessionID),
		sendKey:           bytes.Clone(config.SendKey),
		receiveKey:        bytes.Clone(config.ReceiveKey),
		sendAEAD:          sendAEAD,
		receiveAEAD:       receiveAEAD,
		inactivityTimeout: config.InactivityTimeout,
		now:               config.Now,
		rand:              config.Rand,
		state:             StateActive,
		lastActivity:      now,
		nextLocalSeq:      1,
		seenRemoteSeq:     map[uint64]struct{}{},
	}, nil
}

func (s *StateMachine) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireIfIdle()
	return s.state
}

func (s *StateMachine) SendKey() []byte {
	return bytes.Clone(s.sendKey)
}

func (s *StateMachine) ReceiveKey() []byte {
	return bytes.Clone(s.receiveKey)
}

func (s *StateMachine) InvalidateTrust() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = StateForgotten
}

func (s *StateMachine) Encrypt(messageType protocol.MessageType, plaintext []byte) (EncryptedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureActive(); err != nil {
		return EncryptedMessage{}, err
	}

	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return EncryptedMessage{}, err
	}

	msg := EncryptedMessage{
		Version:   Version,
		SessionID: bytes.Clone(s.sessionID),
		Seq:       s.nextLocalSeq,
		Type:      messageType,
		Nonce:     nonce,
	}
	sealed := s.sendAEAD.Seal(nil, nonce, plaintext, associatedData(msg))
	msg.Ciphertext = bytes.Clone(sealed[:len(sealed)-TagSize])
	msg.Tag = bytes.Clone(sealed[len(sealed)-TagSize:])
	s.nextLocalSeq++
	s.touch()
	return msg, nil
}

func (s *StateMachine) Decrypt(msg EncryptedMessage) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureActive(); err != nil {
		return nil, err
	}
	if msg.Version != Version {
		return nil, fmt.Errorf("unsupported session version %d", msg.Version)
	}
	if !bytes.Equal(msg.SessionID, s.sessionID) {
		return nil, fmt.Errorf("session id mismatch")
	}
	if len(msg.Nonce) != NonceSize {
		return nil, fmt.Errorf("nonce must be %d bytes", NonceSize)
	}
	if len(msg.Tag) != TagSize {
		return nil, fmt.Errorf("tag must be %d bytes", TagSize)
	}
	if _, seen := s.seenRemoteSeq[msg.Seq]; seen {
		return nil, ErrReplay
	}
	expectedSeq := s.lastRemoteSeq + 1
	if msg.Seq != expectedSeq {
		return nil, ErrOutOfOrder
	}

	combined := append(bytes.Clone(msg.Ciphertext), msg.Tag...)
	plaintext, err := s.receiveAEAD.Open(nil, msg.Nonce, combined, associatedData(msg))
	if err != nil {
		return nil, err
	}
	s.lastRemoteSeq = msg.Seq
	s.seenRemoteSeq[msg.Seq] = struct{}{}
	s.touch()
	return plaintext, nil
}

func (s *StateMachine) ensureActive() error {
	s.expireIfIdle()
	switch s.state {
	case StateActive:
		return nil
	case StateForgotten:
		return ErrForgotten
	default:
		return ErrExpired
	}
}

func (s *StateMachine) expireIfIdle() {
	if s.state != StateActive {
		return
	}
	if s.now().Sub(s.lastActivity) > s.inactivityTimeout {
		s.state = StateExpired
	}
}

func (s *StateMachine) touch() {
	s.lastActivity = s.now()
}

func associatedData(msg EncryptedMessage) []byte {
	buffer := bytes.NewBuffer(nil)
	buffer.WriteByte(msg.Version)
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], msg.Seq)
	buffer.Write(seq[:])
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(msg.SessionID)))
	buffer.Write(length[:])
	buffer.Write(msg.SessionID)
	binary.BigEndian.PutUint32(length[:], uint32(len(msg.Type)))
	buffer.Write(length[:])
	buffer.WriteString(string(msg.Type))
	return buffer.Bytes()
}
