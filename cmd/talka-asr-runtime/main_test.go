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

func TestRunServeAcceptsFunASRBinaryFlag(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	// --funasr-binary with a non-existent path should fail at resolveFunASRBinary,
	// not at argument parsing.
	err := run(&out, &errOut, []string{
		"serve",
		"--addr", "127.0.0.1:19095",
		"--funasr-binary", "/nonexistent/funasr-wss-server-2pass",
	})
	if err == nil {
		t.Fatal("run() error = nil, want FunASR binary not found error")
	}
	if strings.Contains(err.Error(), "unknown serve argument") {
		t.Fatalf("run() error = %q, want --funasr-binary to be accepted as a valid flag", err)
	}
	if !strings.Contains(err.Error(), "FunASR binary") {
		t.Fatalf("run() error = %q, want FunASR binary resolution error", err)
	}
}

func TestBuildFunASRArgs(t *testing.T) {
	parentArgs := []string{
		"serve",
		"--addr", "127.0.0.1:19095",
		"--upstream-url", "ws://127.0.0.1:10095/ws",
		"--mode", "2pass",
		"--model-dir", "/models/asr",
		"--online-model-dir", "/models/online",
		"--vad-dir", "/models/vad",
		"--punc-dir", "/models/punc",
		"--itn-dir", "/models/itn",
		"--funasr-binary", "/bin/funasr-wss-server-2pass",
	}

	args := buildFunASRArgs(parentArgs, "127.0.0.1", 12345)

	// Should contain listen-ip and port
	foundListenIP := false
	foundPort := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--listen-ip" && i+1 < len(args) && args[i+1] == "127.0.0.1" {
			foundListenIP = true
		}
		if args[i] == "--port" && i+1 < len(args) && args[i+1] == "12345" {
			foundPort = true
		}
	}
	if !foundListenIP {
		t.Fatalf("buildFunASRArgs() missing --listen-ip 127.0.0.1, got %v", args)
	}
	if !foundPort {
		t.Fatalf("buildFunASRArgs() missing --port 12345, got %v", args)
	}

	// Should forward model dirs
	assertHasFlagPair(t, args, "--model-dir", "/models/asr")
	assertHasFlagPair(t, args, "--online-model-dir", "/models/online")
	assertHasFlagPair(t, args, "--vad-dir", "/models/vad")
	assertHasFlagPair(t, args, "--punc-dir", "/models/punc")
	assertHasFlagPair(t, args, "--itn-dir", "/models/itn")

	// Should default --lm-dir to NONE when not provided by parent
	assertHasFlagPair(t, args, "--lm-dir", "NONE")

	// Should NOT contain Go-proxy-only flags
	for _, arg := range args {
		if arg == "--addr" || arg == "--upstream-url" || arg == "--mode" || arg == "--funasr-binary" || arg == "serve" {
			t.Fatalf("buildFunASRArgs() should not contain Go-proxy-only flag %q, got %v", arg, args)
		}
	}
}

func TestBuildFunASRArgsWithLMDir(t *testing.T) {
	parentArgs := []string{
		"serve",
		"--addr", "127.0.0.1:19095",
		"--model-dir", "/models/asr",
		"--lm-dir", "/models/lm",
	}

	args := buildFunASRArgs(parentArgs, "127.0.0.1", 12345)

	// Should forward the provided --lm-dir, not override with NONE
	assertHasFlagPair(t, args, "--lm-dir", "/models/lm")

	// Should NOT contain --lm-dir NONE
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--lm-dir" && args[i+1] == "NONE" {
			t.Fatalf("buildFunASRArgs() should not add --lm-dir NONE when --lm-dir already provided, got %v", args)
		}
	}
}

func TestResolveFunASRBinaryWithAbsolutePath(t *testing.T) {
	// Non-existent absolute path should error
	_, err := resolveFunASRBinary("/nonexistent/binary")
	if err == nil {
		t.Fatal("resolveFunASRBinary() error = nil, want error for non-existent path")
	}
}

func assertHasFlagPair(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Fatalf("args missing %s %s: %v", flag, value, args)
}
