package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"talka/internal/mdns"
)

type fakePublisher struct {
	mu        sync.Mutex
	started   bool
	stopped   bool
	startErr  error
	startDesc mdns.Descriptor
	startPort int
}

func (p *fakePublisher) Start(ctx context.Context, desc mdns.Descriptor, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = true
	p.startDesc = desc
	p.startPort = port
	if p.startErr != nil {
		return p.startErr
	}
	return nil
}

func (p *fakePublisher) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	return nil
}

func (p *fakePublisher) snapshot() (started bool, stopped bool, desc mdns.Descriptor, port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started, p.stopped, p.startDesc, p.startPort
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
	mustWriteONNXModelFiles(t, root)

	configPath := filepath.Join(root, "config.yaml")
	configBody := testServerConfigBody()
	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &safeTestBuffer{}
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
	mustWriteONNXModelFiles(t, root)

	configPath := filepath.Join(root, "config.yaml")
	configBody := testServerConfigBody()

	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	pub := &fakePublisher{}
	originalFactory := newDiscoveryPublisher
	newDiscoveryPublisher = func() mdns.Publisher { return pub }
	defer func() { newDiscoveryPublisher = originalFactory }()

	ctx, cancel := context.WithCancel(context.Background())
	out := &safeTestBuffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, out, os.Stderr, []string{"--config", configPath, "--listen", "127.0.0.1:0"})
	}()

	baseURL := waitForListenLine(t, out)
	desc, port := waitForPublisherStarted(t, pub)
	if desc.ServiceType != mdns.ServiceType {
		t.Fatalf("publisher ServiceType = %q, want %q", desc.ServiceType, mdns.ServiceType)
	}
	if port <= 0 {
		t.Fatalf("publisher port = %d, want > 0", port)
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
	waitForPublisherStopped(t, pub)
}

func TestRunContinuesWhenDiscoveryPublisherUnavailable(t *testing.T) {
	root := t.TempDir()
	mustWriteONNXModelFiles(t, root)

	configPath := filepath.Join(root, "config.yaml")
	configBody := testServerConfigBody()

	if err := os.WriteFile(configPath, configBody, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	originalFactory := newDiscoveryPublisher
	newDiscoveryPublisher = func() mdns.Publisher { return &fakePublisher{startErr: errors.New("dns-sd unavailable")} }
	defer func() { newDiscoveryPublisher = originalFactory }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &safeTestBuffer{}
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

func mustWriteONNXModelFiles(t *testing.T, root string) {
	t.Helper()
	_ = root
	_ = repoModelDir(t)
}

func testServerConfigBody() []byte {
	modelDir := repoModelDirForServerTest()
	return []byte(fmt.Sprintf(`server:
  bind_host: 127.0.0.1
  port: 0
  service_name: Talka
asr:
  provider: onnx
  host: 127.0.0.1
  port: 10095
  mode: streaming
  sample_rate: 16000
  startup_timeout_seconds: 180
  sherpa_onnx:
    model_profile: paraformer-bilingual
    model_type: paraformer
    precision: int8
    tokens_path: %s
    encoder_path: %s
    decoder_path: %s
    joiner_path: ""
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
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
`, filepath.Join(modelDir, "tokens.txt"), filepath.Join(modelDir, "encoder.int8.onnx"), filepath.Join(modelDir, "decoder.int8.onnx")))
}

func repoModelDir(t *testing.T) string {
	t.Helper()
	dir := repoModelDirForServerTest()
	for _, name := range []string{"tokens.txt", "encoder.int8.onnx", "decoder.int8.onnx"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("required sherpa model %s missing: %v", name, err)
		}
	}
	return dir
}

func repoModelDirForServerTest() string {
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "../..", "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en"))
}

type safeTestBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *safeTestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *safeTestBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func waitForListenLine(t *testing.T, out *safeTestBuffer) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if strings.HasPrefix(line, "LISTEN ") {
				return strings.TrimPrefix(line, "LISTEN ")
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not print LISTEN line: %s", out.String())
	return ""
}

func waitForPublisherStarted(t *testing.T, pub *fakePublisher) (mdns.Descriptor, int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		started, _, desc, port := pub.snapshot()
		if started {
			return desc, port
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("publisher did not start")
	return mdns.Descriptor{}, 0
}

func waitForPublisherStopped(t *testing.T, pub *fakePublisher) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, stopped, _, _ := pub.snapshot()
		if stopped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("publisher did not stop")
}
