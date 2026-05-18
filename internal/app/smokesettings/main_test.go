package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAcceptsPlanASRModelFixturePath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), &stdout, &stderr, []string{
		"--ollama-url", "http://localhost:11434",
		"--model", "qwen3:8b",
		"--asr-model", repoEncoderPath(t),
	})
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "CONFIG_OK ollama_url=http://localhost:11434 model=qwen3:8b asr_model="+repoEncoderPath(t)) {
		t.Fatalf("stdout = %q, want CONFIG_OK for plan fixture path", output)
	}
}

func repoEncoderPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../../..", "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en/encoder.int8.onnx"))
}
