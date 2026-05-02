package mdns

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSmokeTXTInspection(t *testing.T) {
	desc, err := NewDescriptor("Kitchen Speaker", PairingRequired)
	if err != nil {
		t.Fatalf("NewDescriptor(required) error = %v", err)
	}
	if got, want := desc.ServiceType, ServiceType; got != want {
		t.Fatalf("ServiceType = %q, want %q", got, want)
	}

	requiredTXT := desc.TXTRecords()
	if err := ValidateTXT(requiredTXT); err != nil {
		t.Fatalf("ValidateTXT(required) error = %v", err)
	}
	wantRequired := []string{
		"version=1",
		"device_name=Kitchen Speaker",
		"protocol=talka-stream-v1",
		"pairing=required",
	}
	if !reflect.DeepEqual(requiredTXT, wantRequired) {
		t.Fatalf("TXTRecords(required) = %#v, want %#v", requiredTXT, wantRequired)
	}

	pairedDesc, err := desc.WithPairing(PairingPaired)
	if err != nil {
		t.Fatalf("WithPairing(paired) error = %v", err)
	}
	pairedTXT := pairedDesc.TXTRecords()
	if err := ValidateTXT(pairedTXT); err != nil {
		t.Fatalf("ValidateTXT(paired) error = %v", err)
	}
	wantPaired := []string{
		"version=1",
		"device_name=Kitchen Speaker",
		"protocol=talka-stream-v1",
		"pairing=paired",
	}
	if !reflect.DeepEqual(pairedTXT, wantPaired) {
		t.Fatalf("TXTRecords(paired) = %#v, want %#v", pairedTXT, wantPaired)
	}

	for _, state := range []struct {
		name string
		txt  []string
	}{
		{name: string(PairingRequired), txt: requiredTXT},
		{name: string(PairingPaired), txt: pairedTXT},
	} {
		joined := strings.ToLower(strings.Join(state.txt, "\n"))
		for _, forbidden := range []string{"pin", "key", "token", "session", "audio", "transcript", "challenge"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("TXT output for %s contains forbidden term %q: %#v", state.name, forbidden, state.txt)
			}
		}
	}

	fmt.Printf("MDNS_SERVICE service=%s\n", desc.ServiceType)
	fmt.Printf("MDNS_TXT pairing=%s records=%s\n", PairingRequired, strings.Join(requiredTXT, "|"))
	fmt.Printf("MDNS_SECRET_SCAN pairing=%s status=clear\n", PairingRequired)
	fmt.Printf("MDNS_PAIRING_UPDATE from=%s to=%s\n", PairingRequired, PairingPaired)
	fmt.Printf("MDNS_TXT pairing=%s records=%s\n", PairingPaired, strings.Join(pairedTXT, "|"))
	fmt.Printf("MDNS_SECRET_SCAN pairing=%s status=clear\n", PairingPaired)
}
