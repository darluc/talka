package mdns

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// ServiceType is the fixed Bonjour service name for Talka.
	ServiceType = "_talka._tcp"

	protocolVersion = "1"
	protocolName    = "talka-stream-v1"
)

type PairingState string

const (
	PairingRequired PairingState = "required"
	PairingPaired   PairingState = "paired"
)

type Descriptor struct {
	ServiceType string
	DeviceName  string
	Pairing     PairingState
}

func NewDescriptor(deviceName string, pairing PairingState) (Descriptor, error) {
	if err := ValidateDeviceName(deviceName); err != nil {
		return Descriptor{}, err
	}
	if err := validatePairingState(pairing); err != nil {
		return Descriptor{}, err
	}
	return Descriptor{
		ServiceType: ServiceType,
		DeviceName:  deviceName,
		Pairing:     pairing,
	}, nil
}

func (d Descriptor) TXTRecords() []string {
	return []string{
		"version=" + protocolVersion,
		"device_name=" + d.DeviceName,
		"protocol=" + protocolName,
		"pairing=" + string(d.Pairing),
	}
}

func (d Descriptor) WithPairing(pairing PairingState) (Descriptor, error) {
	if err := validatePairingState(pairing); err != nil {
		return Descriptor{}, err
	}
	d.Pairing = pairing
	return d, nil
}

func ValidateTXT(txt []string) error {
	if len(txt) != 4 {
		return fmt.Errorf("txt record must contain exactly 4 entries")
	}

	wantKeys := []string{"version", "device_name", "protocol", "pairing"}
	for i, entry := range txt {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			return fmt.Errorf("txt entry %d must be key=value", i)
		}
		if key != wantKeys[i] {
			return fmt.Errorf("txt entry %d key = %q, want %q", i, key, wantKeys[i])
		}

		switch key {
		case "version":
			if value != protocolVersion {
				return fmt.Errorf("version = %q, want %q", value, protocolVersion)
			}
		case "device_name":
			if err := ValidateDeviceName(value); err != nil {
				return err
			}
		case "protocol":
			if value != protocolName {
				return fmt.Errorf("protocol = %q, want %q", value, protocolName)
			}
		case "pairing":
			if err := validatePairingState(PairingState(value)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected txt key %q", key)
		}
	}

	return nil
}

func ValidateDeviceName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("device name is required")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("device name must not have leading or trailing whitespace")
	}
	lower := strings.ToLower(name)
	for _, term := range []string{"pin", "key", "token", "session", "audio", "transcript", "challenge"} {
		if strings.Contains(lower, term) {
			return fmt.Errorf("device name contains forbidden secret-like term %q", term)
		}
	}
	for _, r := range name {
		if r == '=' || r == 0 || r == '\n' || r == '\r' || r == '\t' {
			return fmt.Errorf("device name contains invalid character")
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("device name contains control characters")
		}
	}
	return nil
}

func validatePairingState(state PairingState) error {
	switch state {
	case PairingRequired, PairingPaired:
		return nil
	default:
		return fmt.Errorf("pairing state must be required or paired")
	}
}
