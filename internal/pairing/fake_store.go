package pairing

import "sync"

type FakeStore struct {
	mu      sync.RWMutex
	local   *LocalIdentity
	trusted map[string]TrustedDevice
}

func NewFakeStore() *FakeStore {
	return &FakeStore{trusted: map[string]TrustedDevice{}}
}

func (s *FakeStore) LoadLocalIdentity() (LocalIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.local == nil {
		return LocalIdentity{}, ErrLocalIdentityNotFound
	}
	identity := cloneLocalIdentity(*s.local)
	return identity, nil
}

func (s *FakeStore) SaveLocalIdentity(identity LocalIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := cloneLocalIdentity(identity)
	s.local = &cloned
	return nil
}

func (s *FakeStore) LoadTrustedDevice(deviceID string) (TrustedDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	device, ok := s.trusted[deviceID]
	if !ok {
		return TrustedDevice{}, ErrUnknownDevice
	}
	return cloneTrustedDevice(device), nil
}

func (s *FakeStore) SaveTrustedDevice(device TrustedDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trusted[device.DeviceID] = cloneTrustedDevice(device)
	return nil
}

func (s *FakeStore) DeleteTrustedDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.trusted, deviceID)
	return nil
}
