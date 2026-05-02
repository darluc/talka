package main

import (
	"bytes"
	"testing"
)

func TestRunPrintsUsageWithoutSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(&stdout, &stderr, nil)
	if err == nil {
		t.Fatal("run() error = nil, want usage error")
	}

	if got := stderr.String(); got == "" {
		t.Fatal("stderr output = empty, want usage text")
	}

	if got := stdout.String(); got != "" {
		t.Fatalf("stdout output = %q, want empty", got)
	}
}

func TestRunSmokeRequiresFixtureFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run(&stdout, &stderr, []string{"smoke"}); err == nil {
		t.Fatal("run() error = nil, want missing fixture error")
	}

	if got := stderr.String(); got == "" {
		t.Fatal("stderr output = empty, want fixture usage text")
	}

	if got := stdout.String(); got != "" {
		t.Fatalf("stdout output = %q, want empty", got)
	}

	if err := run(&stdout, &stderr, []string{"serve", "--help"}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got := stdout.String(); got == "" {
		t.Fatal("stdout output = empty, want serve help text")
	}
}

func TestSmokeUsageDocumentsSampleRateOverride(t *testing.T) {
	if got := smokeUsage(); !bytes.Contains([]byte(got), []byte("--sample-rate")) {
		t.Fatalf("smokeUsage() = %q, want --sample-rate", got)
	}
}
