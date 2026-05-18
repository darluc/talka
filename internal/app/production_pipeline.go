package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"talka/internal/asr"
	"talka/internal/config"
	"talka/internal/inject"
	"talka/internal/llm"
)

var buildPipelineFromConfig = newPipelineFromConfig

func newPipelineFromConfig(cfg config.Config, configDir string) (*Pipeline, error) {
	asrProvider, err := newASRProviderFromConfig(cfg.ASR, configDir)
	if err != nil {
		return nil, err
	}
	llmProvider, err := newLLMProviderFromConfig(cfg.LLM)
	if err != nil {
		return nil, err
	}
	injector, err := newTextInjectorFromConfig(cfg.Injection)
	if err != nil {
		return nil, err
	}
	return NewPipeline(asrProvider, llmProvider, injector), nil
}

func newASRProviderFromConfig(cfg config.ASRConfig, configDir string) (ASRProvider, error) {
	provider := normalizeASRProvider(cfg.Provider)
	if provider == "onnx" {
		encoderPath := sherpaONNXModelPathForPrecision(cfg.SherpaONNX.ModelType, cfg.SherpaONNX.Precision, resolveConfigPath(configDir, cfg.SherpaONNX.EncoderPath))
		decoderPath := sherpaONNXModelPathForPrecision(cfg.SherpaONNX.ModelType, cfg.SherpaONNX.Precision, resolveConfigPath(configDir, cfg.SherpaONNX.DecoderPath))
		return asr.NewSherpaONNXProvider(asr.SherpaONNXConfig{
			ModelType:      cfg.SherpaONNX.ModelType,
			Precision:      cfg.SherpaONNX.Precision,
			TokensPath:     resolveConfigPath(configDir, cfg.SherpaONNX.TokensPath),
			EncoderPath:    encoderPath,
			DecoderPath:    decoderPath,
			JoinerPath:     resolveConfigPath(configDir, cfg.SherpaONNX.JoinerPath),
			NumThreads:     cfg.SherpaONNX.NumThreads,
			DecodingMethod: cfg.SherpaONNX.DecodingMethod,
			FeatureDim:     cfg.SherpaONNX.FeatureDim,
			Provider:       cfg.SherpaONNX.Provider,
		})
	}
	return nil, fmt.Errorf("unsupported asr.provider %q", cfg.Provider)
}

func normalizeASRProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "":
		return "onnx"
	case "onnx", "sherpa", "sherpa_onnx_streaming":
		return "onnx"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func sherpaONNXModelPathForPrecision(modelType, precision, path string) string {
	if strings.TrimSpace(modelType) != "paraformer" {
		return path
	}
	dir := filepath.Dir(path)
	switch strings.TrimSpace(precision) {
	case "fp32":
		switch filepath.Base(path) {
		case "encoder.int8.onnx":
			return filepath.Join(dir, "encoder.onnx")
		case "decoder.int8.onnx":
			return filepath.Join(dir, "decoder.onnx")
		}
	default:
		switch filepath.Base(path) {
		case "encoder.onnx":
			return filepath.Join(dir, "encoder.int8.onnx")
		case "decoder.onnx":
			return filepath.Join(dir, "decoder.int8.onnx")
		}
	}
	return path
}

func newLLMProviderFromConfig(cfg config.LLMConfig) (LLMProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider != "ollama" {
		return nil, fmt.Errorf("unsupported llm.provider %q", cfg.Provider)
	}
	return llm.NewOllamaProvider(llm.OllamaConfig{BaseURL: cfg.BaseURL, Model: cfg.Model, Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}), nil
}

func newTextInjectorFromConfig(cfg config.InjectionConfig) (TextInjector, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "clipboard_paste" {
		return nil, fmt.Errorf("unsupported injection.mode %q", cfg.Mode)
	}
	return inject.NewMacOSPasteInjector(inject.MacOSPasteOptions{RestoreClipboard: cfg.RestoreClipboard}), nil
}

func resolveConfigPath(configDir, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(configDir, value)
}

func resolveOptionalConfigPath(configDir, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return resolveConfigPath(configDir, value)
}
