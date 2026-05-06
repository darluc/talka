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
	asrModel := fs.String("asr-model", "fixtures/config/models/funasr/paraformer-zh-onnx", "ASR model path")
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
	current.Config.ASR.Models.ASR = *asrModel
	updated, statusCode, err := putConfig(server.URL, current.Config)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("settings update returned %d", statusCode)
	}
	_, _ = fmt.Fprintf(stdout, "CONFIG_OK ollama_url=%s model=%s asr_model=%s\n", updated.Config.LLM.BaseURL, updated.Config.LLM.Model, updated.Config.ASR.Models.ASR)

	bad := updated.Config
	bad.ASR.Models.VAD = "models/missing-vad"
	_, statusCode, err = putConfig(server.URL, bad)
	if err != nil {
		return err
	}
	if statusCode != http.StatusBadRequest {
		return fmt.Errorf("invalid settings returned %d, want 400", statusCode)
	}
	_, _ = fmt.Fprintln(stdout, "VALIDATION_ERROR field=asr.models.vad")

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
	for _, dir := range []string{
		"runtime",
		"models/funasr/paraformer-zh-onnx",
		"models/funasr/paraformer-zh-online-onnx",
		"models/funasr/fsmn-vad-onnx",
		"models/funasr/ct-punc-onnx",
		"models/funasr/itn-zh",
		"fixtures/config/models/funasr/paraformer-zh-onnx",
		extraASRModel,
	} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return config.Config{}, "", err
		}
	}
	cfg := config.Default()
	cfg.ASR.RuntimePath = "runtime"
	cfg.ASR.Models.ASR = "models/funasr/paraformer-zh-onnx"
	cfg.ASR.Models.Online = "models/funasr/paraformer-zh-online-onnx"
	cfg.ASR.Models.VAD = "models/funasr/fsmn-vad-onnx"
	cfg.ASR.Models.Punc = "models/funasr/ct-punc-onnx"
	cfg.ASR.Models.ITN = "models/funasr/itn-zh"
	path := filepath.Join(root, "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		return config.Config{}, "", err
	}
	return cfg, path, nil
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
