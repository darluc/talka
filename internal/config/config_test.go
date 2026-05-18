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

func TestDefaultConfigUsesONNXBilingualASR(t *testing.T) {
	cfg := Default()

	if cfg.ASR.Provider != "onnx" {
		t.Fatalf("ASR.Provider = %q, want onnx", cfg.ASR.Provider)
	}
	if cfg.ASR.Mode != "streaming" {
		t.Fatalf("ASR.Mode = %q, want streaming", cfg.ASR.Mode)
	}
	if cfg.ASR.SherpaONNX.ModelProfile != "paraformer-bilingual" {
		t.Fatalf("ModelProfile = %q, want paraformer-bilingual", cfg.ASR.SherpaONNX.ModelProfile)
	}
	if !strings.Contains(cfg.ASR.SherpaONNX.EncoderPath, "streaming-paraformer-bilingual-zh-en") {
		t.Fatalf("EncoderPath = %q, want bilingual model path", cfg.ASR.SherpaONNX.EncoderPath)
	}
	if cfg.ASR.SampleRate != 16000 {
		t.Fatalf("ASR.SampleRate = %d, want 16000", cfg.ASR.SampleRate)
	}
	if cfg.LLM.Provider != "ollama" {
		t.Fatalf("LLM.Provider = %q, want ollama", cfg.LLM.Provider)
	}
}

func TestLoadAcceptsSherpaONNXStreamingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	mustWriteSherpaFiles(t, tmpDir, "models/sherpa", "encoder.onnx", "decoder.onnx")

	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: onnx
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
	if cfg.ASR.Provider != "onnx" {
		t.Fatalf("ASR.Provider = %q, want onnx", cfg.ASR.Provider)
	}
	if cfg.ASR.SherpaONNX.Precision != "int8" {
		t.Fatalf("Precision = %q, want int8 default", cfg.ASR.SherpaONNX.Precision)
	}
	if cfg.ASR.SherpaONNX.ModelProfile != "paraformer-bilingual" {
		t.Fatalf("ModelProfile = %q, want paraformer-bilingual default", cfg.ASR.SherpaONNX.ModelProfile)
	}
}

func TestLoadRejectsFunASRProvider(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: funasr
  host: 127.0.0.1
  port: 10095
  mode: 2pass
  sample_rate: 16000
  startup_timeout_seconds: 180
`))

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want unsupported provider error")
	}
	if got := err.Error(); !containsAll(got, []string{"asr.provider", "must be onnx"}) {
		t.Fatalf("Load() error = %q, want onnx-only provider error", got)
	}
}

func TestLoadAcceptsSherpaONNXFP32Precision(t *testing.T) {
	tmpDir := t.TempDir()
	mustWriteSherpaFiles(t, tmpDir, "models/sherpa", "encoder.onnx", "decoder.onnx")

	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: onnx
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
	mustWriteSherpaFiles(t, tmpDir, "models/sherpa", "encoder.onnx", "decoder.onnx")

	configPath := writeTempConfig(t, tmpDir, []byte(`asr:
  provider: onnx
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
  provider: onnx
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

func mustWriteSherpaFiles(t *testing.T, root, rel, encoder, decoder string) {
	t.Helper()
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", rel, err)
	}
	for _, name := range []string{"tokens.txt", encoder, decoder} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("model"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
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
