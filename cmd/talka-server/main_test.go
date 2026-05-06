package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"talka/internal/mdns"
)

type fakePublisher struct {
	started   bool
	stopped   bool
	startErr  error
	startDesc mdns.Descriptor
	startPort int
}

func (p *fakePublisher) Start(ctx context.Context, desc mdns.Descriptor, port int) error {
	p.started = true
	p.startDesc = desc
	p.startPort = port
	if p.startErr != nil {
		return p.startErr
	}
	return nil
}

func (p *fakePublisher) Stop(ctx context.Context) error {
	p.stopped = true
	return nil
}

func TestRunRejectsInvalidConfigPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := run(ctx, os.Stdout, os.Stderr, []string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil {
		t.Fatal("run() error = nil, want invalid config error")
	}

	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("run() error = %q, want actionable config error", err)
	}
}

func TestRunServesStatusJSON(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "runtime")
	mustMkdir(t, root, "models/asr")
	mustMkdir(t, root, "models/online")
	mustMkdir(t, root, "models/vad")
	mustMkdir(t, root, "models/punc")
	mustMkdir(t, root, "models/itn")

	configPath := filepath.Join(root, "config.yaml")
	configBody := []byte(`server:
  bind_host: 127.0.0.1
  port: 0
  service_name: Talka
asr:
  provider: funasr_onnx
  runtime_path: runtime
  host: 127.0.0.1
  port: 10095
  mode: twopass
  sample_rate: 16000
  models:
    asr: models/asr
    online: models/online
    vad: models/vad
    punc: models/punc
    itn: models/itn
llm:
  provider: ollama
  base_url: http://localhost:11434
  model: qwen3:8b
  timeout_seconds: 30
injection:
  mode: clipboard_paste
  restore_clipboard: true
logging:
  level: info
  capture_audio: false
  capture_transcript: false
`)
	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &strings.Builder{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, out, os.Stderr, []string{"--config", configPath, "--listen", "127.0.0.1:0"})
	}()

	baseURL := waitForListenLine(t, out)
	resp, err := http.Get(baseURL + "/v1/status")
	if err != nil {
		t.Fatalf("GET /v1/status error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload["service_name"] != "Talka" {
		t.Fatalf("service_name = %v, want Talka", payload["service_name"])
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run() error after cancel = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not exit after cancel")
	}
}

func TestRunStartsAndStopsDiscoveryPublisher(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "runtime")
	mustMkdir(t, root, "models/asr")
	mustMkdir(t, root, "models/online")
	mustMkdir(t, root, "models/vad")
	mustMkdir(t, root, "models/punc")
	mustMkdir(t, root, "models/itn")

	configPath := filepath.Join(root, "config.yaml")
	configBody := []byte(`server:
  bind_host: 127.0.0.1
  port: 0
  service_name: Talka
asr:
  provider: funasr_onnx
  runtime_path: runtime
  host: 127.0.0.1
  port: 10095
  mode: twopass
  sample_rate: 16000
  models:
    asr: models/asr
    online: models/online
    vad: models/vad
    punc: models/punc
    itn: models/itn
llm:
  provider: ollama
  base_url: http://localhost:11434
  model: qwen3:8b
  timeout_seconds: 30
injection:
  mode: clipboard_paste
  restore_clipboard: true
logging:
  level: info
  capture_audio: false
  capture_transcript: false
`)

	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	pub := &fakePublisher{}
	originalFactory := newDiscoveryPublisher
	newDiscoveryPublisher = func() mdns.Publisher { return pub }
	defer func() { newDiscoveryPublisher = originalFactory }()

	ctx, cancel := context.WithCancel(context.Background())
	out := &strings.Builder{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, out, os.Stderr, []string{"--config", configPath, "--listen", "127.0.0.1:0"})
	}()

	baseURL := waitForListenLine(t, out)
	if !pub.started {
		t.Fatal("publisher did not start")
	}
	if pub.startDesc.ServiceType != mdns.ServiceType {
		t.Fatalf("publisher ServiceType = %q, want %q", pub.startDesc.ServiceType, mdns.ServiceType)
	}
	if pub.startPort <= 0 {
		t.Fatalf("publisher port = %d, want > 0", pub.startPort)
	}

	resp, err := http.Get(baseURL + "/v1/status")
	if err != nil {
		t.Fatalf("GET /v1/status error = %v", err)
	}
	resp.Body.Close()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run() error after cancel = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not exit after cancel")
	}
	if !pub.stopped {
		t.Fatal("publisher did not stop")
	}
}

func TestRunContinuesWhenDiscoveryPublisherUnavailable(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "runtime")
	mustMkdir(t, root, "models/asr")
	mustMkdir(t, root, "models/online")
	mustMkdir(t, root, "models/vad")
	mustMkdir(t, root, "models/punc")
	mustMkdir(t, root, "models/itn")

	configPath := filepath.Join(root, "config.yaml")
	configBody := []byte(`server:
  bind_host: 127.0.0.1
  port: 0
  service_name: Talka
asr:
  provider: funasr_onnx
  runtime_path: runtime
  host: 127.0.0.1
  port: 10095
  mode: twopass
  sample_rate: 16000
  models:
    asr: models/asr
    online: models/online
    vad: models/vad
    punc: models/punc
    itn: models/itn
llm:
  provider: ollama
  base_url: http://localhost:11434
  model: qwen3:8b
  timeout_seconds: 30
injection:
  mode: clipboard_paste
  restore_clipboard: true
logging:
  level: info
  capture_audio: false
  capture_transcript: false
`)

	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	originalFactory := newDiscoveryPublisher
	newDiscoveryPublisher = func() mdns.Publisher { return &fakePublisher{startErr: errors.New("dns-sd unavailable")} }
	defer func() { newDiscoveryPublisher = originalFactory }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &strings.Builder{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, out, os.Stderr, []string{"--config", configPath, "--listen", "127.0.0.1:0"})
	}()

	baseURL := waitForListenLine(t, out)
	resp, err := http.Get(baseURL + "/v1/status")
	if err != nil {
		t.Fatalf("GET /v1/status error = %v", err)
	}
	resp.Body.Close()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run() error after cancel = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not exit after cancel")
	}
}

func TestBuildDiscoveryEvidenceUsesServiceNameListenerPortAndSafeTxt(t *testing.T) {
	got, err := buildDiscoveryEvidence("Talka", 41234)
	if err != nil {
		t.Fatalf("buildDiscoveryEvidence() error = %v", err)
	}

	if got.Descriptor.ServiceType != mdns.ServiceType {
		t.Fatalf("Descriptor.ServiceType = %q, want %q", got.Descriptor.ServiceType, mdns.ServiceType)
	}
	if got.Port != 41234 {
		t.Fatalf("Port = %d, want %d", got.Port, 41234)
	}
	if got.Descriptor.DeviceName != "Talka" {
		t.Fatalf("Descriptor.DeviceName = %q, want %q", got.Descriptor.DeviceName, "Talka")
	}
	if got.Descriptor.Pairing != mdns.PairingRequired {
		t.Fatalf("Descriptor.Pairing = %q, want %q", got.Descriptor.Pairing, mdns.PairingRequired)
	}
	if err := mdns.ValidateTXT(got.Descriptor.TXTRecords()); err != nil {
		t.Fatalf("ValidateTXT() error = %v", err)
	}

	joined := strings.ToLower(strings.Join(got.Descriptor.TXTRecords(), "|"))
	for _, forbidden := range []string{"pin", "key", "token", "session", "audio", "transcript", "challenge"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("TXTRecords() contains forbidden term %q: %#v", forbidden, got.Descriptor.TXTRecords())
		}
	}
}

func mustMkdir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", rel, err)
	}
}

func waitForListenLine(t *testing.T, out *strings.Builder) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text := strings.TrimSpace(out.String())
		if strings.HasPrefix(text, "LISTEN ") {
			return strings.TrimPrefix(text, "LISTEN ")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not print LISTEN line: %s", out.String())
	return ""
}
