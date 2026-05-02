//go:build darwin

package pairing

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const keychainServiceName = "talka.trusted-device"

type commandRunner func(name string, args ...string) ([]byte, error)

type DarwinStore struct {
	runner commandRunner
}

func NewDarwinStore() *DarwinStore {
	return &DarwinStore{runner: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}}
}

func (s *DarwinStore) LoadLocalIdentity() (LocalIdentity, error) {
	return s.loadLocalIdentityAccount("local-identity")
}

func (s *DarwinStore) SaveLocalIdentity(identity LocalIdentity) error {
	return s.saveAccount("local-identity", identity)
}

func (s *DarwinStore) LoadTrustedDevice(deviceID string) (TrustedDevice, error) {
	var device TrustedDevice
	if err := s.loadAccount("device:"+deviceID, ErrUnknownDevice, &device); err != nil {
		return TrustedDevice{}, err
	}
	return device, nil
}

func (s *DarwinStore) SaveTrustedDevice(device TrustedDevice) error {
	return s.saveAccount("device:"+device.DeviceID, device)
}

func (s *DarwinStore) DeleteTrustedDevice(deviceID string) error {
	output, err := s.runner("security", "delete-generic-password", "-a", "device:"+deviceID, "-s", keychainServiceName)
	if isKeychainNotFound(err, output) {
		return nil
	}
	return err
}

func (s *DarwinStore) loadLocalIdentityAccount(account string) (LocalIdentity, error) {
	var identity LocalIdentity
	if err := s.loadAccount(account, ErrLocalIdentityNotFound, &identity); err != nil {
		return LocalIdentity{}, err
	}
	return identity, nil
}

func (s *DarwinStore) saveAccount(account string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	_, err = s.runner("security", "add-generic-password", "-a", account, "-s", keychainServiceName, "-w", encoded, "-U")
	return err
}

func (s *DarwinStore) loadAccount(account string, notFoundErr error, target any) error {
	output, err := s.runner("security", "find-generic-password", "-a", account, "-s", keychainServiceName, "-w")
	if err != nil {
		if isKeychainNotFound(err, output) {
			return notFoundErr
		}
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil {
		return fmt.Errorf("decode keychain payload: %w", err)
	}
	return json.Unmarshal(decoded, target)
}

func isKeychainNotFound(err error, output []byte) bool {
	if err == nil {
		return false
	}
	message := err.Error() + "\n" + string(output)
	return strings.Contains(message, "could not be found") || strings.Contains(message, "The specified item could not be found")
}
