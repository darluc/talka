package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunAcceptsPlanASRModelFixturePath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), &stdout, &stderr, []string{
		"--ollama-url", "http://localhost:11434",
		"--model", "qwen3:8b",
		"--asr-model", "fixtures/models/fake",
	})
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "CONFIG_OK ollama_url=http://localhost:11434 model=qwen3:8b asr_model=fixtures/models/fake") {
		t.Fatalf("stdout = %q, want CONFIG_OK for plan fixture path", output)
	}
}
