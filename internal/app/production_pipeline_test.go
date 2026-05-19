package app

import (
	"path/filepath"
	"testing"

	"talka/internal/asr"
	"talka/internal/config"
	"talka/internal/inject"
)

func TestNewASRProviderFromConfigBuildsSherpaONNXProvider(t *testing.T) {
	root := t.TempDir()
	modelDir := repoModelDir(t)
	cfg := config.ASRConfig{
		Provider:       "onnx",
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "streaming",
		SampleRate:     16000,
		StartupTimeout: 180,
		SherpaONNX: config.SherpaONNXConfig{
			ModelProfile:   "paraformer-bilingual",
			ModelType:      "paraformer",
			Precision:      "int8",
			TokensPath:     filepath.Join(modelDir, "tokens.txt"),
			EncoderPath:    filepath.Join(modelDir, "encoder.int8.onnx"),
			DecoderPath:    filepath.Join(modelDir, "decoder.int8.onnx"),
			NumThreads:     2,
			DecodingMethod: "greedy_search",
			FeatureDim:     80,
			Provider:       "cpu",
		},
	}

	provider, err := newASRProviderFromConfig(cfg, root)
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}
	if _, ok := provider.(*asr.SherpaONNXProvider); !ok {
		t.Fatalf("provider type = %T, want *asr.SherpaONNXProvider", provider)
	}
}

func TestNewTextInjectorFromConfigSupportsKeyPress(t *testing.T) {
	injector, err := newTextInjectorFromConfig(config.InjectionConfig{Mode: "clipboard_paste"})
	if err != nil {
		t.Fatalf("newTextInjectorFromConfig() error = %v", err)
	}
	if _, ok := injector.(inject.KeyPressDriver); !ok {
		t.Fatalf("newTextInjectorFromConfig() = %T, want inject.KeyPressDriver", injector)
	}
}

func TestNewASRProviderFromConfigRejectsFunASRProvider(t *testing.T) {
	cfg := config.Default()
	cfg.ASR.Provider = "funasr"

	_, err := newASRProviderFromConfig(cfg.ASR, t.TempDir())
	if err == nil {
		t.Fatal("newASRProviderFromConfig() error = nil, want unsupported provider error")
	}
}

func TestNormalizeASRProviderDefaultsToONNX(t *testing.T) {
	for _, provider := range []string{"", "onnx", "sherpa", "sherpa_onnx_streaming"} {
		if got := normalizeASRProvider(provider); got != "onnx" {
			t.Fatalf("normalizeASRProvider(%q) = %q, want onnx", provider, got)
		}
	}
}

func TestSherpaONNXModelPathForPrecisionSwitchesParaformerFiles(t *testing.T) {
	root := t.TempDir()
	int8Encoder := filepath.Join(root, "encoder.int8.onnx")
	int8Decoder := filepath.Join(root, "decoder.int8.onnx")
	fp32Encoder := filepath.Join(root, "encoder.onnx")
	fp32Decoder := filepath.Join(root, "decoder.onnx")

	if got := sherpaONNXModelPathForPrecision("paraformer", "fp32", int8Encoder); got != fp32Encoder {
		t.Fatalf("fp32 encoder = %q, want %q", got, fp32Encoder)
	}
	if got := sherpaONNXModelPathForPrecision("paraformer", "fp32", int8Decoder); got != fp32Decoder {
		t.Fatalf("fp32 decoder = %q, want %q", got, fp32Decoder)
	}
	if got := sherpaONNXModelPathForPrecision("paraformer", "int8", fp32Encoder); got != int8Encoder {
		t.Fatalf("int8 encoder = %q, want %q", got, int8Encoder)
	}
	if got := sherpaONNXModelPathForPrecision("paraformer", "int8", fp32Decoder); got != int8Decoder {
		t.Fatalf("int8 decoder = %q, want %q", got, int8Decoder)
	}
	if got := sherpaONNXModelPathForPrecision("transducer", "fp32", int8Encoder); got != int8Encoder {
		t.Fatalf("transducer path = %q, want unchanged %q", got, int8Encoder)
	}
}
