package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Issue struct {
	Field   string `json:"field" yaml:"field"`
	Value   string `json:"value,omitempty" yaml:"value,omitempty"`
	Problem string `json:"problem" yaml:"problem"`
}

type ValidationError struct {
	Issues []Issue `json:"issues" yaml:"issues"`
}

func (e ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "config validation failed"
	}

	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if issue.Value == "" {
			parts = append(parts, fmt.Sprintf("%s: %s", issue.Field, issue.Problem))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s (%s)", issue.Field, issue.Problem, issue.Value))
	}
	return strings.Join(parts, "; ")
}

func Load(path string) (Config, error) {
	if path == "" {
		cfg := Default()
		if err := cfg.Validate("."); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer file.Close()

	cfg := Default()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode config %s: %w", path, err)
	}

	if err := cfg.Validate(filepath.Dir(path)); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := cfg.Validate(filepath.Dir(path)); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory %s: %w", filepath.Dir(path), err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("encode config %s: %w", path, err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}

	return nil
}

func (cfg Config) Validate(baseDir string) error {
	var issues []Issue

	appendIssue := func(field, value, problem string) {
		issues = append(issues, Issue{Field: field, Value: value, Problem: problem})
	}

	if strings.TrimSpace(cfg.Server.BindHost) == "" {
		appendIssue("server.bind_host", cfg.Server.BindHost, "must not be empty")
	}
	if strings.TrimSpace(cfg.Server.ServiceName) == "" {
		appendIssue("server.service_name", cfg.Server.ServiceName, "must not be empty")
	}

	if strings.TrimSpace(cfg.ASR.Provider) == "" {
		appendIssue("asr.provider", cfg.ASR.Provider, "must not be empty")
	}
	if strings.TrimSpace(cfg.ASR.RuntimePath) == "" {
		appendIssue("asr.runtime_path", cfg.ASR.RuntimePath, "must not be empty")
	} else if err := mustExist(baseDir, cfg.ASR.RuntimePath); err != nil {
		appendIssue("asr.runtime_path", cfg.ASR.RuntimePath, err.Error())
	}
	if strings.TrimSpace(cfg.ASR.Host) == "" {
		appendIssue("asr.host", cfg.ASR.Host, "must not be empty")
	}
	if cfg.ASR.Port <= 0 {
		appendIssue("asr.port", fmt.Sprintf("%d", cfg.ASR.Port), "must be greater than zero")
	}
	if strings.TrimSpace(cfg.ASR.Mode) == "" {
		appendIssue("asr.mode", cfg.ASR.Mode, "must not be empty")
	}
	if cfg.ASR.SampleRate != 16000 {
		appendIssue("asr.sample_rate", fmt.Sprintf("%d", cfg.ASR.SampleRate), "must be 16000 for the local ASR contract")
	}
	validatePath := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			appendIssue(field, value, "must not be empty")
			return
		}
		if err := mustExist(baseDir, value); err != nil {
			appendIssue(field, value, err.Error())
		}
	}
	validatePath("asr.models.asr", cfg.ASR.Models.ASR)
	validatePath("asr.models.vad", cfg.ASR.Models.VAD)
	validatePath("asr.models.punc", cfg.ASR.Models.Punc)
	validatePath("asr.models.itn", cfg.ASR.Models.ITN)

	if strings.TrimSpace(cfg.LLM.Provider) == "" {
		appendIssue("llm.provider", cfg.LLM.Provider, "must not be empty")
	}
	if strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		appendIssue("llm.base_url", cfg.LLM.BaseURL, "must not be empty")
	}
	if strings.TrimSpace(cfg.LLM.Model) == "" {
		appendIssue("llm.model", cfg.LLM.Model, "must not be empty")
	}
	if cfg.LLM.TimeoutSeconds <= 0 {
		appendIssue("llm.timeout_seconds", fmt.Sprintf("%d", cfg.LLM.TimeoutSeconds), "must be greater than zero")
	}

	if strings.TrimSpace(cfg.Injection.Mode) == "" {
		appendIssue("injection.mode", cfg.Injection.Mode, "must not be empty")
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Level)) {
	case "debug", "info", "warn", "error":
	default:
		appendIssue("logging.level", cfg.Logging.Level, "must be one of debug, info, warn, error")
	}

	if len(issues) > 0 {
		return ValidationError{Issues: issues}
	}
	return nil
}

func mustExist(baseDir, value string) error {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("must exist at %s", path)
		}
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	return nil
}
