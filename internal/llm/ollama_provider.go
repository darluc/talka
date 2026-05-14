package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOllamaBaseURL = "http://localhost:11434"
	defaultOllamaModel   = "qwen3:8b"
	defaultOllamaTimeout = 30 * time.Second
)

type cleanupMode string

const (
	cleanupModeSegment cleanupMode = "segment"
	cleanupModeFinal   cleanupMode = "final"
)

type OllamaConfig struct {
	BaseURL    string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type OllamaProvider struct {
	baseURL    string
	model      string
	timeout    time.Duration
	httpClient *http.Client
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Think    bool                `json:"think"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
}

func NewOllamaProvider(config OllamaConfig) *OllamaProvider {
	if strings.TrimSpace(config.BaseURL) == "" {
		config.BaseURL = defaultOllamaBaseURL
	}
	if strings.TrimSpace(config.Model) == "" {
		config.Model = defaultOllamaModel
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultOllamaTimeout
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: config.Timeout}
	}

	return &OllamaProvider{
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		model:      config.Model,
		timeout:    config.Timeout,
		httpClient: config.HTTPClient,
	}
}

func (p *OllamaProvider) Cleanup(ctx context.Context, transcript string) (Result, error) {
	raw := strings.TrimSpace(transcript)
	if raw == "" {
		return Result{Text: "", RawTranscript: "", Status: StatusCleaned, Provider: "ollama"}, nil
	}

	cleaned, err := p.CleanupStrict(ctx, raw)
	if err != nil {
		status := StatusFallbackError
		if isTimeoutLike(err) {
			status = StatusFallbackTimeout
		}
		return Result{Text: raw, RawTranscript: raw, Status: status, Provider: "ollama"}, nil
	}

	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return Result{Text: raw, RawTranscript: raw, Status: StatusFallbackError, Provider: "ollama"}, nil
	}

	return Result{Text: cleaned, RawTranscript: raw, Status: StatusCleaned, Provider: "ollama"}, nil
}

func (p *OllamaProvider) CleanupStrict(ctx context.Context, transcript string) (string, error) {
	raw := strings.TrimSpace(transcript)
	requestBody := ollamaChatRequest{
		Model:    p.model,
		Messages: promptForCleanup(cleanupModeFinal, raw),
		Stream:   false,
		Think:    false,
	}
	cleaned, err := p.chat(ctx, requestBody)
	if err != nil {
		return "", err
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", errors.New("ollama returned empty cleanup text")
	}
	return cleaned, nil
}

func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(body))
		if message == "" {
			return fmt.Errorf("ollama /api/tags at %s returned %s", p.baseURL, resp.Status)
		}
		return fmt.Errorf("ollama /api/tags at %s returned %s: %s", p.baseURL, resp.Status, message)
	}
	return nil
}

func (p *OllamaProvider) chat(ctx context.Context, requestBody ollamaChatRequest) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(body))
		if message == "" {
			return "", fmt.Errorf("ollama /api/chat at %s returned %s for model %s", p.baseURL, resp.Status, p.model)
		}
		return "", fmt.Errorf("ollama /api/chat at %s returned %s for model %s: %s", p.baseURL, resp.Status, p.model, message)
	}

	var decoded ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}

	return decoded.Message.Content, nil
}

func promptForCleanup(mode cleanupMode, transcript string) []ollamaChatMessage {
	systemPrompt := finalCleanupSystemPrompt
	if mode == cleanupModeSegment {
		systemPrompt = segmentCleanupSystemPrompt
	}

	return []ollamaChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: strings.TrimSpace(transcript)},
	}
}

func isTimeoutLike(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

const segmentCleanupSystemPrompt = "You are a strict dictation segment cleaner. Add only light punctuation, simple spacing normalization, and obvious duplicate cleanup when necessary. Do not add facts. Do not change meaning. Do not summarize. Do not use markdown. Do not include explanations or commentary. Output only the cleaned segment text."

const finalCleanupSystemPrompt = "You are a strict dictation finalizer. Your job is to merge the dictated text into the final insertable result while preserving the speaker's exact meaning and tone. Do not add facts. Do not change meaning. Do not summarize unless explicitly asked. Do not use markdown. Do not include explanations or commentary. Output only the final cleaned text."
