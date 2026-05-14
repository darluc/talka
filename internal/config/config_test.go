package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPathUsesApplicationSupportTalka(t *testing.T) {
	home := "/Users/example"
	want := "/Users/example/Library/Application Support/Talka/config.yaml"

	if got := DefaultPath(home); got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestDefaultConfigMatchesScaffoldConstraints(t *testing.T) {
	cfg := Default()

	if cfg.Server.BindHost != "0.0.0.0" {
		t.Fatalf("Server.BindHost = %q, want %q", cfg.Server.BindHost, "0.0.0.0")
	}

	if cfg.Server.ServiceName != "Talka" {
		t.Fatalf("Server.ServiceName = %q, want %q", cfg.Server.ServiceName, "Talka")
	}

	if cfg.ASR.Provider != "funasr_embedded" {
		t.Fatalf("ASR.Provider = %q, want %q", cfg.ASR.Provider, "funasr_embedded")
	}

	if cfg.ASR.SampleRate != 16000 {
		t.Fatalf("ASR.SampleRate = %d, want %d", cfg.ASR.SampleRate, 16000)
	}
	if cfg.ASR.StartupTimeout != 180 {
		t.Fatalf("ASR.StartupTimeout = %d, want %d", cfg.ASR.StartupTimeout, 180)
	}

	if cfg.ASR.Mode != "2pass" {
		t.Fatalf("ASR.Mode = %q, want %q", cfg.ASR.Mode, "2pass")
	}

	if cfg.ASR.RuntimePath == "" {
		t.Fatal("ASR.RuntimePath is empty")
	}

	if cfg.ASR.Models.ASR != "models/funasr/paraformer-zh-onnx" {
		t.Fatalf("ASR.Models.ASR = %q, want embedded model path", cfg.ASR.Models.ASR)
	}

	if cfg.ASR.Models.Online != "models/funasr/paraformer-zh-online-onnx" {
		t.Fatalf("ASR.Models.Online = %q, want embedded online model path", cfg.ASR.Models.Online)
	}

	if cfg.LLM.Provider != "ollama" {
		t.Fatalf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "ollama")
	}

	if cfg.LLM.BaseURL != "http://localhost:11434" {
		t.Fatalf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, "http://localhost:11434")
	}

	if !cfg.Injection.RestoreClipboard {
		t.Fatal("Injection.RestoreClipboard = false, want true")
	}

	if cfg.Logging.CaptureAudio {
		t.Fatal("Logging.CaptureAudio = true, want false")
	}

	if cfg.Logging.CaptureTranscript {
		t.Fatal("Logging.CaptureTranscript = true, want false")
	}
}

func TestLoadAppliesDefaultsAndValidatesEmbeddedRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	mustMkdir(t, tmpDir, "runtime")
	mustMkdir(t, tmpDir, "models/asr")
	mustMkdir(t, tmpDir, "models/online")
	mustMkdir(t, tmpDir, "models/vad")
	mustMkdir(t, tmpDir, "models/punc")
	mustMkdir(t, tmpDir, "models/itn")

	configPath := writeTempConfig(t, tmpDir, []byte(`server:
  port: 0
asr:
  provider: funasr_embedded
  runtime_path: runtime
  port: 10095
  startup_timeout_seconds: 180
  models:
    asr: models/asr
    online: models/online
    vad: models/vad
    punc: models/punc
    itn: models/itn
`))

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.BindHost != "0.0.0.0" {
		t.Fatalf("Server.BindHost = %q, want %q", cfg.Server.BindHost, "0.0.0.0")
	}

	if cfg.LLM.Provider != "ollama" {
		t.Fatalf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "ollama")
	}

	if cfg.ASR.RuntimePath != "runtime" {
		t.Fatalf("ASR.RuntimePath = %q, want %q", cfg.ASR.RuntimePath, "runtime")
	}

	if cfg.ASR.Host != "127.0.0.1" {
		t.Fatalf("ASR.Host = %q, want %q", cfg.ASR.Host, "127.0.0.1")
	}

	if cfg.ASR.Models.Online != "models/online" {
		t.Fatalf("ASR.Models.Online = %q, want embedded online model path", cfg.ASR.Models.Online)
	}
}

func TestLoadRejectsMissingEmbeddedOnlineModel(t *testing.T) {
	tmpDir := t.TempDir()
	mustMkdir(t, tmpDir, "runtime")
	mustMkdir(t, tmpDir, "models/asr")
	mustMkdir(t, tmpDir, "models/vad")
	mustMkdir(t, tmpDir, "models/punc")
	mustMkdir(t, tmpDir, "models/itn")
	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: funasr_embedded
  runtime_path: runtime
  port: 10095
  startup_timeout_seconds: 180
  models:
    asr: models/asr
    online: ""
    vad: models/vad
    punc: models/punc
    itn: models/itn
`))

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want missing model path error")
	}

	if got := err.Error(); got == "" || !containsAll(got, []string{"asr.models.online", "must not be empty"}) {
		t.Fatalf("Load() error = %q, want actionable online model error", got)
	}
}

func TestLoadAllowsExternalFunASRWithoutLocalRuntimeAssets(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: funasr_external
  host: 127.0.0.1
  port: 10095
  mode: 2pass
  sample_rate: 16000
  startup_timeout_seconds: 180
`))

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ASR.Provider != "funasr_external" {
		t.Fatalf("ASR.Provider = %q, want %q", cfg.ASR.Provider, "funasr_external")
	}
}

func TestLoadAcceptsSherpaONNXStreamingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	mustMkdir(t, tmpDir, "models/sherpa")
	for _, name := range []string{"tokens.txt", "encoder.onnx", "decoder.onnx"} {
		if err := os.WriteFile(filepath.Join(tmpDir, "models/sherpa", name), []byte("model"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: sherpa_onnx_streaming
  host: 127.0.0.1
  port: 10095
  mode: streaming
  sample_rate: 16000
  startup_timeout_seconds: 180
  sherpa_onnx:
    model_type: paraformer
    tokens_path: models/sherpa/tokens.txt
    encoder_path: models/sherpa/encoder.onnx
    decoder_path: models/sherpa/decoder.onnx
    joiner_path: ""
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
`))

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ASR.Provider != "sherpa_onnx_streaming" {
		t.Fatalf("ASR.Provider = %q, want sherpa_onnx_streaming", cfg.ASR.Provider)
	}
	if cfg.ASR.SherpaONNX.TokensPath != "models/sherpa/tokens.txt" {
		t.Fatalf("TokensPath = %q, want configured path", cfg.ASR.SherpaONNX.TokensPath)
	}
	if cfg.ASR.SherpaONNX.ModelType != "paraformer" {
		t.Fatalf("ModelType = %q, want paraformer", cfg.ASR.SherpaONNX.ModelType)
	}
	if cfg.ASR.SherpaONNX.Precision != "int8" {
		t.Fatalf("Precision = %q, want int8 default", cfg.ASR.SherpaONNX.Precision)
	}
}

func TestLoadAcceptsSherpaONNXFP32Precision(t *testing.T) {
	tmpDir := t.TempDir()
	mustMkdir(t, tmpDir, "models/sherpa")
	for _, name := range []string{"tokens.txt", "encoder.onnx", "decoder.onnx"} {
		if err := os.WriteFile(filepath.Join(tmpDir, "models/sherpa", name), []byte("model"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: sherpa_onnx_streaming
  host: 127.0.0.1
  port: 10095
  mode: streaming
  sample_rate: 16000
  startup_timeout_seconds: 180
  sherpa_onnx:
    model_type: paraformer
    precision: fp32
    tokens_path: models/sherpa/tokens.txt
    encoder_path: models/sherpa/encoder.onnx
    decoder_path: models/sherpa/decoder.onnx
    joiner_path: ""
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
`))

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ASR.SherpaONNX.Precision != "fp32" {
		t.Fatalf("Precision = %q, want fp32", cfg.ASR.SherpaONNX.Precision)
	}
}

func TestLoadRejectsInvalidSherpaONNXPrecision(t *testing.T) {
	tmpDir := t.TempDir()
	mustMkdir(t, tmpDir, "models/sherpa")
	for _, name := range []string{"tokens.txt", "encoder.onnx", "decoder.onnx"} {
		if err := os.WriteFile(filepath.Join(tmpDir, "models/sherpa", name), []byte("model"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: sherpa_onnx_streaming
  host: 127.0.0.1
  port: 10095
  mode: streaming
  sample_rate: 16000
  startup_timeout_seconds: 180
  sherpa_onnx:
    model_type: paraformer
    precision: half
    tokens_path: models/sherpa/tokens.txt
    encoder_path: models/sherpa/encoder.onnx
    decoder_path: models/sherpa/decoder.onnx
    joiner_path: ""
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
`))

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want invalid precision error")
	}
	if got := err.Error(); !containsAll(got, []string{"asr.sherpa_onnx.precision", "must be one of int8, fp32"}) {
		t.Fatalf("Load() error = %q, want precision validation error", got)
	}
}

func TestLoadRejectsMissingSherpaONNXModelFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: sherpa_onnx_streaming
  host: 127.0.0.1
  port: 10095
  mode: streaming
  sample_rate: 16000
  startup_timeout_seconds: 180
  sherpa_onnx:
    model_type: paraformer
    tokens_path: models/sherpa/tokens.txt
    encoder_path: models/sherpa/encoder.onnx
    decoder_path: models/sherpa/decoder.onnx
    joiner_path: ""
    num_threads: 2
    decoding_method: greedy_search
    feature_dim: 80
    provider: cpu
`))

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want missing sherpa model error")
	}
	if got := err.Error(); !containsAll(got, []string{"asr.sherpa_onnx.tokens_path", "must exist"}) {
		t.Fatalf("Load() error = %q, want sherpa model path error", got)
	}
}

func mustMkdir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", rel, err)
	}
}

func writeTempConfig(t *testing.T, root string, body []byte) string {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func containsAll(got string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(got, part) {
			return false
		}
	}
	return true
}
