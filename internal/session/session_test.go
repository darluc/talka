package session

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestStateMachineRoundTripsEncryptedMessages(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	client, server := newLinkedStateMachines(t, clock)

	msg, err := client.Encrypt(protocol.MessageTypePing, []byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	plaintext, err := server.Decrypt(msg)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if !bytes.Equal(plaintext, []byte("hello")) {
		t.Fatalf("plaintext = %q, want %q", plaintext, []byte("hello"))
	}
	if got := server.State(); got != StateActive {
		t.Fatalf("State() = %q, want %q", got, StateActive)
	}
}

func TestStateMachineRejectsReplayedSequence(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	client, server := newLinkedStateMachines(t, clock)

	msg, err := client.Encrypt(protocol.MessageTypePing, []byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if _, err := server.Decrypt(msg); err != nil {
		t.Fatalf("Decrypt(first) error = %v", err)
	}

	_, err = server.Decrypt(msg)
	if !errors.Is(err, ErrReplay) {
		t.Fatalf("Decrypt(replay) error = %v, want %v", err, ErrReplay)
	}
}

func TestStateMachineRejectsOutOfOrderSequence(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	_, server := newLinkedStateMachines(t, clock)

	first := EncryptedMessage{Version: 1, SessionID: []byte("session-12345678"), Seq: 1, Type: protocol.MessageTypePing, Nonce: bytes.Repeat([]byte{1}, NonceSize), Ciphertext: []byte{1}, Tag: bytes.Repeat([]byte{2}, TagSize)}
	second := EncryptedMessage{Version: 1, SessionID: []byte("session-12345678"), Seq: 3, Type: protocol.MessageTypePing, Nonce: bytes.Repeat([]byte{3}, NonceSize), Ciphertext: []byte{4}, Tag: bytes.Repeat([]byte{5}, TagSize)}

	server.lastRemoteSeq = 1
	server.seenRemoteSeq[1] = struct{}{}

	_, err := server.Decrypt(second)
	if !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("Decrypt(out of order) error = %v, want %v", err, ErrOutOfOrder)
	}
	_ = first
}

func TestStateMachineExpiresAfterInactivity(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	client, _ := newLinkedStateMachines(t, clock)

	clock.Advance(31 * time.Second)

	_, err := client.Encrypt(protocol.MessageTypePing, []byte("late"))
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("Encrypt() error = %v, want %v", err, ErrExpired)
	}
	if got := client.State(); got != StateExpired {
		t.Fatalf("State() = %q, want %q", got, StateExpired)
	}
}

func TestStateMachineSupportsConcurrentStateAndEncryption(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	client, _ := newLinkedStateMachines(t, clock)

	const workers = 8
	const messagesPerWorker = 25
	var wg sync.WaitGroup
	sequences := make(chan uint64, workers*messagesPerWorker)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range messagesPerWorker {
				msg, err := client.Encrypt(protocol.MessageTypePing, []byte("hello"))
				if err != nil {
					t.Errorf("Encrypt() error = %v", err)
					return
				}
				sequences <- msg.Seq
				if got := client.State(); got != StateActive {
					t.Errorf("State() = %q, want %q", got, StateActive)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(sequences)
	seen := map[uint64]struct{}{}
	for sequence := range sequences {
		if _, ok := seen[sequence]; ok {
			t.Fatalf("duplicate sequence %d", sequence)
		}
		seen[sequence] = struct{}{}
	}
	if got, want := len(seen), workers*messagesPerWorker; got != want {
		t.Fatalf("encrypted messages = %d, want %d", got, want)
	}
}

func newLinkedStateMachines(t *testing.T, clock *fakeClock) (*StateMachine, *StateMachine) {
	t.Helper()

	client, err := NewStateMachine(Config{SessionID: []byte("session-12345678"), SendKey: bytes.Repeat([]byte{1}, 32), ReceiveKey: bytes.Repeat([]byte{2}, 32), InactivityTimeout: 30 * time.Second, Now: clock.Now})
	if err != nil {
		t.Fatalf("NewStateMachine(client) error = %v", err)
	}
	server, err := NewStateMachine(Config{SessionID: []byte("session-12345678"), SendKey: bytes.Repeat([]byte{2}, 32), ReceiveKey: bytes.Repeat([]byte{1}, 32), InactivityTimeout: 30 * time.Second, Now: clock.Now})
	if err != nil {
		t.Fatalf("NewStateMachine(server) error = %v", err)
	}
	return client, server
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
