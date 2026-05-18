package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"talka/internal/app"
	"talka/internal/config"
)

func main() {
	if err := run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "smokesettings: %v\n", err)
		os.Exit(1)
	}
}

func run(_ context.Context, stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("smokesettings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ollamaURL := fs.String("ollama-url", "http://localhost:11434", "Ollama base URL")
	model := fs.String("model", "qwen3:8b", "Ollama model")
	asrModel := fs.String("asr-model", "fixtures/config/models/sherpa-onnx/streaming-paraformer-bilingual-zh-en/encoder.int8.onnx", "Sherpa ONNX encoder path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, cfgPath, err := smokeConfig(*asrModel)
	if err != nil {
		return err
	}
	service, err := app.New(cfg, cfgPath, nil)
	if err != nil {
		return err
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	current, err := fetchConfig(server.URL)
	if err != nil {
		return err
	}
	current.Config.LLM.BaseURL = *ollamaURL
	current.Config.LLM.Model = *model
	current.Config.ASR.SherpaONNX.EncoderPath = *asrModel
	updated, statusCode, err := putConfig(server.URL, current.Config)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("settings update returned %d", statusCode)
	}
	_, _ = fmt.Fprintf(stdout, "CONFIG_OK ollama_url=%s model=%s asr_model=%s\n", updated.Config.LLM.BaseURL, updated.Config.LLM.Model, updated.Config.ASR.SherpaONNX.EncoderPath)

	bad := updated.Config
	bad.ASR.SherpaONNX.DecoderPath = "models/missing-decoder.onnx"
	_, statusCode, err = putConfig(server.URL, bad)
	if err != nil {
		return err
	}
	if statusCode != http.StatusBadRequest {
		return fmt.Errorf("invalid settings returned %d, want 400", statusCode)
	}
	_, _ = fmt.Fprintln(stdout, "VALIDATION_ERROR field=asr.sherpa_onnx.decoder_path")

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	if containsSecretField(data) {
		return errors.New("config file contains secret-looking fields")
	}
	_, _ = fmt.Fprintln(stdout, "SECRET_SCAN_OK")
	return nil
}

func fetchConfig(baseURL string) (app.ConfigResponse, error) {
	resp, err := http.Get(baseURL + "/v1/config")
	if err != nil {
		return app.ConfigResponse{}, err
	}
	defer resp.Body.Close()
	var payload app.ConfigResponse
	return payload, json.NewDecoder(resp.Body).Decode(&payload)
}

func putConfig(baseURL string, cfg config.Config) (app.ConfigResponse, int, error) {
	body, err := json.Marshal(cfg)
	if err != nil {
		return app.ConfigResponse{}, 0, err
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+"/v1/config", bytes.NewReader(body))
	if err != nil {
		return app.ConfigResponse{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return app.ConfigResponse{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return app.ConfigResponse{}, resp.StatusCode, nil
	}
	var payload app.ConfigResponse
	return payload, resp.StatusCode, json.NewDecoder(resp.Body).Decode(&payload)
}

func smokeConfig(extraASRModel string) (config.Config, string, error) {
	root, err := os.MkdirTemp("", "talka-settings-smoke-")
	if err != nil {
		return config.Config{}, "", err
	}
	modelDir := repoModelDir()
	fixtureDir := "fixtures/config/models/sherpa-onnx/streaming-paraformer-bilingual-zh-en"
	for _, dir := range []string{fixtureDir, filepath.Dir(extraASRModel)} {
		if strings.TrimSpace(dir) != "" {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				return config.Config{}, "", err
			}
		}
	}
	for _, file := range []string{
		filepath.Join(fixtureDir, "encoder.int8.onnx"),
		extraASRModel,
	} {
		if strings.TrimSpace(file) != "" {
			if err := os.WriteFile(filepath.Join(root, file), []byte("model"), 0o644); err != nil {
				return config.Config{}, "", err
			}
		}
	}
	cfg := config.Default()
	cfg.ASR.SherpaONNX.TokensPath = filepath.Join(modelDir, "tokens.txt")
	cfg.ASR.SherpaONNX.EncoderPath = filepath.Join(modelDir, "encoder.int8.onnx")
	cfg.ASR.SherpaONNX.DecoderPath = filepath.Join(modelDir, "decoder.int8.onnx")
	path := filepath.Join(root, "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		return config.Config{}, "", err
	}
	return cfg, path, nil
}

func repoModelDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en"
	}
	return filepath.Clean(filepath.Join(wd, "../../..", "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en"))
}

func containsSecretField(data []byte) bool {
	lower := strings.ToLower(string(data))
	for _, term := range []string{"pin:", "token:", "secret:", "send_key:", "receive_key:", "session_key:"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}
