package app

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"talka/internal/asr"
	"talka/internal/config"
	"talka/internal/inject"
	"talka/internal/llm"
	"talka/internal/protocol"
)

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
	if provider != "funasr_onnx" {
		return nil, fmt.Errorf("unsupported asr.provider %q", cfg.Provider)
	}
	manager := asr.NewRuntimeManager(asr.RuntimeManagerConfig{
		RuntimePath: resolveConfigPath(configDir, cfg.RuntimePath),
		RuntimeArgs: asrRuntimeArgsFromConfig(cfg),
		Host:        cfg.Host,
		Port:        cfg.Port,
		Models: asr.ModelPaths{
			ASR:  resolveConfigPath(configDir, cfg.Models.ASR),
			VAD:  resolveConfigPath(configDir, cfg.Models.VAD),
			Punc: resolveConfigPath(configDir, cfg.Models.Punc),
			ITN:  resolveConfigPath(configDir, cfg.Models.ITN),
		},
	})
	return asr.NewManagedProvider(manager, asr.Config{Version: protocol.VersionV1Alpha1}), nil
}

func asrWebsocketURL(host string, port int) string {
	return fmt.Sprintf("ws://%s:%d/ws", host, port)
}

func asrRuntimeArgsFromConfig(cfg config.ASRConfig) []string {
	return []string{"serve", "--addr", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), "--mode", cfg.Mode}
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
