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
	"talka/internal/protocol"
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
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "sidecar" {
		return asr.NewSidecarProvider(asr.Config{URL: asrWebsocketURL(cfg.Host, cfg.Port), Version: protocol.VersionV1Alpha1}), nil
	}
	if provider == "sherpa_onnx_streaming" {
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
	if provider == "funasr_external" {
		return asr.NewUpstreamProvider(nil, asr.UpstreamProviderConfig{
			URL:     fmt.Sprintf("ws://%s:%d/ws", cfg.Host, cfg.Port),
			Mode:    cfg.Mode,
			Timeout: 5 * time.Second,
		}), nil
	}
	if provider == "funasr_container" {
		manager := asr.NewDockerRuntimeManager(asr.DockerRuntimeManagerConfig{
			Host:          cfg.Host,
			Port:          cfg.Port,
			Mode:          cfg.Mode,
			Image:         cfg.ContainerImage,
			ContainerName: cfg.ContainerName,
			DownloadDir:   resolveConfigPath(configDir, cfg.DownloadDir),
			HotwordPath:   resolveOptionalConfigPath(configDir, cfg.HotwordPath),
			Models: asr.ModelPaths{
				ASR:    cfg.Models.ASR,
				Online: cfg.Models.Online,
				VAD:    cfg.Models.VAD,
				Punc:   cfg.Models.Punc,
				ITN:    cfg.Models.ITN,
				LM:     cfg.Models.LM,
			},
			StartupTimeout: time.Duration(cfg.StartupTimeout) * time.Second,
		})
		return asr.NewUpstreamProvider(manager, asr.UpstreamProviderConfig{
			URL:     fmt.Sprintf("ws://%s:%d/ws", cfg.Host, cfg.Port),
			Mode:    cfg.Mode,
			Timeout: 5 * time.Second,
		}), nil
	}
	if provider != "funasr_onnx" && provider != "funasr_embedded" {
		return nil, fmt.Errorf("unsupported asr.provider %q", cfg.Provider)
	}
	isEmbedded := provider == "funasr_embedded"
	// Resolve FunASR binary path relative to config dir before building args.
	if cfg.FunASRBinaryPath != "" {
		cfg.FunASRBinaryPath = resolveOptionalConfigPath(configDir, cfg.FunASRBinaryPath)
	}
	manager := asr.NewUpstreamRuntimeManager(asr.UpstreamRuntimeManagerConfig{
		RuntimePath:     resolveConfigPath(configDir, cfg.RuntimePath),
		RuntimeArgs:     asrRuntimeArgsFromConfig(cfg),
		Host:            cfg.Host,
		Port:            cfg.Port,
		AlwaysEphemeral: isEmbedded,
		HotwordPath:     resolveOptionalConfigPath(configDir, cfg.HotwordPath),
		Models: asr.ModelPaths{
			ASR:    resolveConfigPath(configDir, cfg.Models.ASR),
			Online: resolveConfigPath(configDir, cfg.Models.Online),
			VAD:    resolveConfigPath(configDir, cfg.Models.VAD),
			Punc:   resolveConfigPath(configDir, cfg.Models.Punc),
			ITN:    resolveConfigPath(configDir, cfg.Models.ITN),
			LM:     resolveOptionalConfigPath(configDir, cfg.Models.LM),
		},
		StartupTimeout: time.Duration(cfg.StartupTimeout) * time.Second,
	})
	if isEmbedded {
		return asr.NewManagedSidecarProvider(manager, asr.Config{
			Version: protocol.VersionV1Alpha1,
			Timeout: 5 * time.Second,
		}), nil
	}

	return asr.NewUpstreamProvider(manager, asr.UpstreamProviderConfig{
		URL:     asrWebsocketURL(cfg.Host, cfg.Port),
		Mode:    cfg.Mode,
		Timeout: 5 * time.Second,
	}), nil
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

func asrWebsocketURL(host string, port int) string {
	return fmt.Sprintf("ws://%s:%d/ws", host, port)
}

func asrRuntimeArgsFromConfig(cfg config.ASRConfig) []string {
	args := []string{
		"serve",
		"--addr", fmt.Sprintf("%s:%d", cfg.Host, asrProxyPort),
		"--upstream-url", asrWebsocketURL(cfg.Host, cfg.Port),
		"--mode", cfg.Mode,
		"--model-dir", cfg.Models.ASR,
		"--online-model-dir", cfg.Models.Online,
		"--vad-dir", cfg.Models.VAD,
		"--punc-dir", cfg.Models.Punc,
		"--itn-dir", cfg.Models.ITN,
	}
	if cfg.FunASRBinaryPath != "" {
		args = append(args, "--funasr-binary", cfg.FunASRBinaryPath)
	}
	return args
}

// asrProxyPort is the default port where the talka-asr-runtime proxy listens.
// The proxy forwards WebSocket connections to the upstream FunASR service
// running on the configured ASR port (cfg.Port, typically 10095).
// Using a separate port avoids a circular reference where the proxy
// would connect to itself instead of the upstream.
const asrProxyPort = 19095

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
