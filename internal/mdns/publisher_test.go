package mdns

import (
	"context"
	"errors"
	"slices"
	"strings"
	"reflect"
	"testing"
)

type fakeRunner struct {
	name  string
	args  []string
	proc  *fakeProcess
	err   error
}

func (r *fakeRunner) Start(name string, args ...string) (Process, error) {
	r.name = name
	r.args = slices.Clone(args)
	if r.err != nil {
		return nil, r.err
	}
	if r.proc == nil {
		r.proc = &fakeProcess{}
	}
	return r.proc, nil
}

type fakeProcess struct {
	killed bool
	waited bool
}

func (p *fakeProcess) Kill() error {
	p.killed = true
	return nil
}

func (p *fakeProcess) Wait() error {
	p.waited = true
	return nil
}

func TestPublisherStartUsesDnsSdWithServiceTypePortAndTxt(t *testing.T) {
	desc, err := NewDescriptor("Kitchen Speaker", PairingRequired)
	if err != nil {
		t.Fatalf("NewDescriptor() error = %v", err)
	}

	runner := &fakeRunner{}
	publisher := NewPublisher(runner)
	if err := publisher.Start(context.Background(), desc, 41234); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if got, want := runner.name, "dns-sd"; got != want {
		t.Fatalf("runner name = %q, want %q", got, want)
	}
	wantArgs := []string{"-R", "Kitchen Speaker", ServiceType, "local", "41234", "version=1", "device_name=Kitchen Speaker", "protocol=talka-stream-v1", "pairing=required"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("runner args = %#v, want %#v", runner.args, wantArgs)
	}
	if runner.proc == nil || runner.proc.killed {
		t.Fatalf("process state = %#v, want started and not killed", runner.proc)
	}

	if err := publisher.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !runner.proc.killed {
		t.Fatal("Stop() did not kill process")
	}
	if !runner.proc.waited {
		t.Fatal("Stop() did not wait for process")
	}
}

func TestPublisherStopWithoutStartIsNoop(t *testing.T) {
	publisher := NewPublisher(&fakeRunner{})
	if err := publisher.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestPublisherStartSurfacesRunnerFailure(t *testing.T) {
	desc, err := NewDescriptor("Kitchen Speaker", PairingRequired)
	if err != nil {
		t.Fatalf("NewDescriptor() error = %v", err)
	}

	wantErr := errors.New("dns-sd missing")
	publisher := NewPublisher(&fakeRunner{err: wantErr})
	if err := publisher.Start(context.Background(), desc, 41234); !errors.Is(err, ErrBonjourUnavailable) {
		t.Fatalf("Start() error = %v, want bonjour unavailable", err)
	} else if got := err.Error(); !strings.Contains(got, "dns-sd missing") {
		t.Fatalf("Start() error text = %q, want dns-sd missing", got)
	}
}
