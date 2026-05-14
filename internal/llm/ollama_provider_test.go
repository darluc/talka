package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOllamaProviderUsesStrictChatCleanupContract(t *testing.T) {
	rawBytes, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "asr", "zh_raw.txt"))
	if err != nil {
		t.Fatalf("ReadFile(raw) error = %v", err)
	}
	cleanBytes, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "llm", "zh_clean.txt"))
	if err != nil {
		t.Fatalf("ReadFile(clean) error = %v", err)
	}

	raw := strings.TrimSpace(string(rawBytes))
	want := strings.TrimSpace(string(cleanBytes))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %s, want /api/chat", r.URL.Path)
		}

		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if think, ok := requestBody["think"].(bool); !ok || think {
			t.Fatalf("think = %#v, want false", requestBody["think"])
		}

		body, err := json.Marshal(requestBody)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var req ollamaChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if req.Model != "qwen3:8b" {
			t.Fatalf("Model = %q, want qwen3:8b", req.Model)
		}
		if req.Stream {
			t.Fatal("Stream = true, want false")
		}
		if len(req.Messages) != 2 {
			t.Fatalf("len(Messages) = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Fatalf("Messages[0].Role = %q, want system", req.Messages[0].Role)
		}
		if req.Messages[1].Role != "user" {
			t.Fatalf("Messages[1].Role = %q, want user", req.Messages[1].Role)
		}
		if req.Messages[1].Content != raw {
			t.Fatalf("Messages[1].Content = %q, want %q", req.Messages[1].Content, raw)
		}

		mustContainAll(t, req.Messages[0].Content, []string{
			"The input text is produced by ASR",
			"fix obvious speech-recognition mistakes",
			"mixed Chinese-English recognition errors",
			"Do not add facts.",
			"Do not change meaning.",
			"Do not use markdown.",
			"Do not include explanations or commentary.",
			"Output only the final cleaned text.",
		})

		_ = json.NewEncoder(w).Encode(ollamaChatResponse{
			Message: ollamaChatMessage{Role: "assistant", Content: "  " + want + "\n"},
			Done:    true,
		})
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{BaseURL: server.URL, Model: "qwen3:8b", Timeout: time.Second})
	result, err := provider.Cleanup(context.Background(), raw)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if got := result.Status; got != StatusCleaned {
		t.Fatalf("Status = %q, want %q", got, StatusCleaned)
	}
	if got := result.Text; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if got := result.RawTranscript; got != raw {
		t.Fatalf("RawTranscript = %q, want %q", got, raw)
	}
	if got := result.Provider; got != "ollama" {
		t.Fatalf("Provider = %q, want ollama", got)
	}
}

func TestOllamaProviderHealthCheckUsesTagsEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{BaseURL: server.URL, Model: "qwen3:8b", Timeout: time.Second})

	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if gotPath != "/api/tags" {
		t.Fatalf("HealthCheck path = %q, want /api/tags", gotPath)
	}
}

func TestOllamaProviderHealthCheckReportsUnhealthyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "model store unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{BaseURL: server.URL, Model: "qwen3:8b", Timeout: time.Second})

	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck() error = nil, want unhealthy status")
	}
	if !strings.Contains(err.Error(), "model store unavailable") {
		t.Fatalf("HealthCheck() error = %q, want response diagnostic", err)
	}
}

func TestOllamaProviderReturnsTypedTimeoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()

	provider := NewOllamaProvider(OllamaConfig{BaseURL: server.URL, Model: "qwen3:8b", Timeout: 20 * time.Millisecond})
	result, err := provider.Cleanup(context.Background(), "你好世界")
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if got := result.Status; got != StatusFallbackTimeout {
		t.Fatalf("Status = %q, want %q", got, StatusFallbackTimeout)
	}
	if got := result.Text; got != "你好世界" {
		t.Fatalf("Text = %q, want raw fallback", got)
	}
	if got := result.RawTranscript; got != "你好世界" {
		t.Fatalf("RawTranscript = %q, want raw transcript", got)
	}
}

func TestCleanupPromptsStayStrictForSegmentAndFinalModes(t *testing.T) {
	segment := promptForCleanup(cleanupModeSegment, "你好世界")
	final := promptForCleanup(cleanupModeFinal, "今天天气不错我们下午两点开会")

	mustContainAll(t, segment[0].Content, []string{
		"The input text is produced by ASR",
		"fix obvious speech-recognition mistakes",
		"mixed Chinese-English recognition errors",
		"light punctuation",
		"Do not add facts.",
		"Output only the cleaned segment text.",
	})
	mustContainAll(t, final[0].Content, []string{
		"merge the dictated text into the final insertable result",
		"The input text is produced by ASR",
		"fix obvious speech-recognition mistakes",
		"mixed Chinese-English recognition errors",
		"Do not use markdown.",
		"Output only the final cleaned text.",
	})
}

func mustContainAll(t *testing.T, got string, parts []string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(got, part) {
			t.Fatalf("%q does not contain %q", got, part)
		}
	}
}
