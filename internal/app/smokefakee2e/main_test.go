package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"talka/internal/asr"
)

func TestRunReportsASRFailureWithoutInsert(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), &stdout, &stderr, []string{"--fixture", "../../../fixtures/audio/zh-short.pcm", "--full-session", "--asr-unavailable"})
	if err == nil {
		t.Fatal("run() error = nil, want ASR failure")
	}
	if got := stdout.String(); !strings.Contains(got, "EXPECTED_ERROR stage=asr") {
		t.Fatalf("stdout = %q, want ASR expected error marker", got)
	}
	if got := stdout.String(); strings.Contains(got, "INSERT_OK") {
		t.Fatalf("stdout = %q, ASR failure must not insert", got)
	}
}

func TestRunReportsInsertionFailureWithRecoverableFinalText(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), &stdout, &stderr, []string{"--fixture", "../../../fixtures/audio/zh-short.pcm", "--full-session", "--insertion-failure"})
	if err == nil {
		t.Fatal("run() error = nil, want insertion failure")
	}
	output := stdout.String()
	if !strings.Contains(output, "EXPECTED_ERROR stage=insertion") {
		t.Fatalf("stdout = %q, want insertion expected error marker", output)
	}
	if !strings.Contains(output, "RECOVERABLE_TEXT 你好，世界") {
		t.Fatalf("stdout = %q, want recoverable final text", output)
	}
}

func TestRunWritesDebugWAVFromEncryptedFullSession(t *testing.T) {
	debugWAV := filepath.Join(t.TempDir(), "task-11-debug.wav")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), &stdout, &stderr, []string{"--fixture", "../../../fixtures/audio/zh-short.pcm", "--full-session", "--debug-wav", debugWAV})
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "DEBUG_WAV_OK path=") {
		t.Fatalf("stdout = %q, want debug wav marker", output)
	}
	data, err := os.ReadFile(debugWAV)
	if err != nil {
		t.Fatalf("ReadFile(debug wav) error = %v", err)
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("debug wav header = %q/%q, want RIFF/WAVE", data[0:4], data[8:12])
	}
	if got := binary.LittleEndian.Uint16(data[22:24]); got != 1 {
		t.Fatalf("channels = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != 16000 {
		t.Fatalf("sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint16(data[34:36]); got != 16 {
		t.Fatalf("bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got == 0 {
		t.Fatalf("data chunk length = %d, want non-zero", got)
	}
}

func TestRunHostIntegrationFullSessionUsesExternalASRURLAndWAVFixture(t *testing.T) {
	runtime := &asr.FakeRuntime{Ready: true}
	server := httptest.NewServer(runtime.Handler())
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), &stdout, &stderr, []string{
		"--fixture", "../../../fixtures/audio/zh-short.wav",
		"--fixture-format", "wav",
		"--full-session",
		"--host-integration",
		"--asr-url", "ws" + strings.TrimPrefix(server.URL, "http") + "/ws",
		"--asr-timeout", (2 * time.Second).String(),
		"--llm-provider", "fake",
	})
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "HOST_FULL_SESSION kind=host-integration physical_ios=false asr=external llm=fake injection=fake") {
		t.Fatalf("stdout = %q, want host integration marker", output)
	}
	if !strings.Contains(output, "ENCRYPTED_AUDIO_START sample_rate=16000 channels=1 encoding=pcm_s16le frame_ms=20") {
		t.Fatalf("stdout = %q, want encrypted audio start marker", output)
	}
	if !strings.Contains(output, "INSERT_OK target=fake status=inserted") {
		t.Fatalf("stdout = %q, want fake insertion marker", output)
	}
}
