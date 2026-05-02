package mdns

import (
	"reflect"
	"testing"
)

func TestDescriptorBuildsExactServiceTypeAndTxt(t *testing.T) {
	desc, err := NewDescriptor("Kitchen Speaker", PairingRequired)
	if err != nil {
		t.Fatalf("NewDescriptor() error = %v", err)
	}

	if got, want := desc.ServiceType, ServiceType; got != want {
		t.Fatalf("ServiceType = %q, want %q", got, want)
	}

	txt := desc.TXTRecords()
	want := []string{
		"version=1",
		"device_name=Kitchen Speaker",
		"protocol=talka-stream-v1",
		"pairing=required",
	}
	if !reflect.DeepEqual(txt, want) {
		t.Fatalf("TXTRecords() = %#v, want %#v", txt, want)
	}

	if err := ValidateTXT(txt); err != nil {
		t.Fatalf("ValidateTXT() error = %v", err)
	}
}

func TestPairingStateUpdateIsDeterministic(t *testing.T) {
	desc, err := NewDescriptor("Desk Hub", PairingRequired)
	if err != nil {
		t.Fatalf("NewDescriptor() error = %v", err)
	}

	updated, err := desc.WithPairing(PairingPaired)
	if err != nil {
		t.Fatalf("WithPairing() error = %v", err)
	}

	want := []string{
		"version=1",
		"device_name=Desk Hub",
		"protocol=talka-stream-v1",
		"pairing=paired",
	}
	if got := updated.TXTRecords(); !reflect.DeepEqual(got, want) {
		t.Fatalf("updated.TXTRecords() = %#v, want %#v", got, want)
	}

	back, err := updated.WithPairing(PairingRequired)
	if err != nil {
		t.Fatalf("WithPairing() revert error = %v", err)
	}
	if got := back.TXTRecords(); !reflect.DeepEqual(got, []string{
		"version=1",
		"device_name=Desk Hub",
		"protocol=talka-stream-v1",
		"pairing=required",
	}) {
		t.Fatalf("reverted TXTRecords() = %#v", got)
	}
}

func TestValidateRejectsSecretLikeFieldsAndSmuggling(t *testing.T) {
	for name, txt := range map[string][]string{
		"secret field": {"version=1", "pin=123456", "protocol=talka-stream-v1", "pairing=required"},
		"unknown session field": {"version=1", "device_name=Hub", "protocol=talka-stream-v1", "session_id=abc"},
		"key material": {"version=1", "device_name=Hub", "protocol=talka-stream-v1", "public_key=abcd"},
		"audio field": {"version=1", "device_name=Hub", "protocol=talka-stream-v1", "audio=pcm"},
		"challenge field": {"version=1", "device_name=Hub", "protocol=talka-stream-v1", "challenge=xyz"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTXT(txt); err == nil {
				t.Fatalf("ValidateTXT() error = nil, want secret hygiene failure")
			}
		})
	}

	if err := ValidateDeviceName("Printer=pin"); err == nil {
		t.Fatalf("ValidateDeviceName() error = nil, want smuggling rejection")
	}
}

func TestValidateRejectsSecretLikeDeviceNames(t *testing.T) {
	for _, name := range []string{
		"PIN 123456",
		"session token",
		"audio transcript",
		"challenge key",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			if err := ValidateDeviceName(name); err == nil {
				t.Fatalf("ValidateDeviceName(%q) error = nil, want secret-term rejection", name)
			}
		})
	}

	if err := ValidateTXT([]string{
		"version=1",
		"device_name=Kitchen Speaker",
		"protocol=talka-stream-v1",
		"pairing=required",
	}); err != nil {
		t.Fatalf("ValidateTXT() valid record error = %v", err)
	}
}
