//go:build darwin

package pairing

import (
	"errors"
	"testing"
)

func TestDarwinKeychainNotFoundUsesCommandOutput(t *testing.T) {
	runner := func(string, ...string) ([]byte, error) {
		return []byte("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n"), errors.New("exit status 44")
	}
	store := &DarwinStore{runner: runner}

	_, err := store.LoadLocalIdentity()
	if !errors.Is(err, ErrLocalIdentityNotFound) {
		t.Fatalf("LoadLocalIdentity() error = %v, want %v", err, ErrLocalIdentityNotFound)
	}

	_, err = store.LoadTrustedDevice("ios-device")
	if !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("LoadTrustedDevice() error = %v, want %v", err, ErrUnknownDevice)
	}
}
