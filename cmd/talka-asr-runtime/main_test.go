package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsUsageWithoutSubcommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := run(&out, &errOut, nil)
	if err == nil {
		t.Fatal("run() error = nil, want missing subcommand error")
	}
	if !strings.Contains(errOut.String(), "usage: talka-asr-runtime") {
		t.Fatalf("stderr = %q, want usage", errOut.String())
	}
}

func TestValidateLocalAddrRejectsRemoteBindings(t *testing.T) {
	err := validateLocalAddr("0.0.0.0:19095")
	if err == nil {
		t.Fatal("validateLocalAddr() error = nil, want localhost-only validation error")
	}
	if !strings.Contains(err.Error(), "localhost-only") {
		t.Fatalf("error = %q, want localhost-only guidance", err)
	}
}

func TestRunSmokeAcceptsCrashMidstreamFlag(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := run(&out, &errOut, []string{"smoke", "--fixture", "missing.wav", "--crash-midstream"})
	if err == nil {
		t.Fatal("run() error = nil, want missing fixture error")
	}
	if strings.Contains(err.Error(), "unknown smoke argument") {
		t.Fatalf("run() error = %q, want supported crash-midstream flag", err)
	}
	if !strings.Contains(err.Error(), "missing.wav") {
		t.Fatalf("run() error = %q, want fixture validation after parsing crash flag", err)
	}
}
