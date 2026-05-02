//go:build !darwin

package pairing

import "fmt"

type DarwinStore struct{}

func NewDarwinStore() *DarwinStore {
	return &DarwinStore{}
}

func (s *DarwinStore) LoadLocalIdentity() (LocalIdentity, error) {
	return LocalIdentity{}, fmt.Errorf("darwin keychain is unavailable on this platform")
}

func (s *DarwinStore) SaveLocalIdentity(LocalIdentity) error {
	return fmt.Errorf("darwin keychain is unavailable on this platform")
}

func (s *DarwinStore) LoadTrustedDevice(string) (TrustedDevice, error) {
	return TrustedDevice{}, fmt.Errorf("darwin keychain is unavailable on this platform")
}

func (s *DarwinStore) SaveTrustedDevice(TrustedDevice) error {
	return fmt.Errorf("darwin keychain is unavailable on this platform")
}

func (s *DarwinStore) DeleteTrustedDevice(string) error {
	return fmt.Errorf("darwin keychain is unavailable on this platform")
}
