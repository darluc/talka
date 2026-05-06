package asr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"talka/internal/protocol"
)

const (
	defaultStartupTimeout = 30 * time.Second
	defaultStopTimeout    = 2 * time.Second
	defaultHealthTimeout  = 500 * time.Millisecond
)

type ModelPaths struct {
	ASR    string
	Online string
	VAD    string
	Punc   string
	ITN    string
	LM     string
}

type RuntimeManagerConfig struct {
	RuntimePath    string
	RuntimeArgs    []string
	Host           string
	Port           int
	Models         ModelPaths
	Env            []string
	StartupTimeout time.Duration
	StopTimeout    time.Duration
	HealthTimeout  time.Duration
	Stdout         io.Writer
	Stderr         io.Writer
}

type RuntimeManager struct {
	config RuntimeManagerConfig

	mu  sync.Mutex
	cmd *exec.Cmd
	url string
}

type ManagedProvider struct {
	manager      *RuntimeManager
	clientConfig Config
}

func NewRuntimeManager(config RuntimeManagerConfig) *RuntimeManager {
	if config.StartupTimeout <= 0 {
		config.StartupTimeout = defaultStartupTimeout
	}
	if config.StopTimeout <= 0 {
		config.StopTimeout = defaultStopTimeout
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = defaultHealthTimeout
	}
	return &RuntimeManager{config: config, url: websocketURLFor(config.Host, config.Port)}
}

func NewManagedProvider(manager *RuntimeManager, clientConfig Config) *ManagedProvider {
	return &ManagedProvider{manager: manager, clientConfig: clientConfig}
}

func (cfg RuntimeManagerConfig) Validate() error {
	var issues []string

	if strings.TrimSpace(cfg.RuntimePath) == "" {
		issues = append(issues, "runtime_path must not be empty")
	} else if err := mustExistPath(cfg.RuntimePath); err != nil {
		issues = append(issues, "runtime_path "+err.Error())
	}

	if !isLocalHost(cfg.Host) {
		issues = append(issues, fmt.Sprintf("host must stay localhost-only, got %q", cfg.Host))
	}
	if cfg.Port <= 0 {
		issues = append(issues, fmt.Sprintf("port must be greater than zero (%d)", cfg.Port))
	}

	for field, value := range map[string]string{
		"models.asr":    cfg.Models.ASR,
		"models.online": cfg.Models.Online,
		"models.vad":    cfg.Models.VAD,
		"models.punc":   cfg.Models.Punc,
		"models.itn":    cfg.Models.ITN,
	} {
		if strings.TrimSpace(value) == "" {
			issues = append(issues, field+" must not be empty")
			continue
		}
		if err := mustExistPath(value); err != nil {
			issues = append(issues, field+" "+err.Error())
		}
	}
	if strings.TrimSpace(cfg.Models.LM) != "" {
		if err := mustExistPath(cfg.Models.LM); err != nil {
			issues = append(issues, "models.lm "+err.Error())
		}
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func (m *RuntimeManager) URL() string {
	return m.url
}

func (m *RuntimeManager) EnsureRunning(ctx context.Context) error {
	if err := m.config.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()

	if cmd != nil && m.healthCheck(ctx) == nil {
		return nil
	}

	return m.Restart(ctx)
}

func (m *RuntimeManager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}

func (m *RuntimeManager) Start(ctx context.Context) error {
	if err := m.config.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	if m.cmd != nil {
		m.mu.Unlock()
		if err := m.healthCheck(ctx); err == nil {
			return nil
		}
		return m.Restart(ctx)
	}

	cmd := exec.Command(m.config.RuntimePath, m.config.RuntimeArgs...)
	cmd.Env = append(os.Environ(), m.config.Env...)
	cmd.Stdout = m.config.Stdout
	cmd.Stderr = m.config.Stderr
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.cmd = cmd
	m.mu.Unlock()

	go m.waitForExit(cmd)

	deadline := time.Now().Add(m.config.StartupTimeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			_ = m.Stop(context.Background())
			return err
		}
		if err := m.healthCheck(ctx); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	_ = m.Stop(context.Background())
	return fmt.Errorf("runtime did not become healthy at %s within %s", m.url, m.config.StartupTimeout)
}

func (m *RuntimeManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.Wait() }()

	stopTimeout := m.config.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = defaultStopTimeout
	}

	select {
	case err := <-stopped:
		m.clearCommand(cmd)
		if err != nil && !errors.Is(err, os.ErrProcessDone) && !isExitInterrupt(err) {
			return err
		}
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		m.clearCommand(cmd)
		return ctx.Err()
	case <-time.After(stopTimeout):
		_ = cmd.Process.Kill()
		m.clearCommand(cmd)
		return nil
	}
}

func (m *RuntimeManager) waitForExit(cmd *exec.Cmd) {
	_ = cmd.Wait()
	m.clearCommand(cmd)
}

func (m *RuntimeManager) clearCommand(cmd *exec.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == cmd {
		m.cmd = nil
	}
}

func (m *RuntimeManager) healthCheck(ctx context.Context) error {
	client := NewClient(Config{URL: m.url, Version: protocol.VersionV1Alpha1, Timeout: m.config.HealthTimeout})
	return client.HealthCheck(ctx)
}

func (p *ManagedProvider) Transcribe(ctx context.Context, request Request) (Result, error) {
	if err := p.manager.EnsureRunning(ctx); err != nil {
		return Result{}, err
	}

	clientConfig := p.clientConfig
	clientConfig.URL = p.manager.URL()
	client := NewClient(clientConfig)

	stream, err := client.Transcribe(ctx, request.Metadata, request.Frames)
	if err != nil {
		if isRuntimeUnavailable(err) {
			restartCtx, cancel := context.WithTimeout(context.Background(), p.manager.config.StartupTimeout)
			defer cancel()
			_ = p.manager.Restart(restartCtx)
			return Result{}, newRuntimeUnavailableError("ASR runtime became unavailable", err)
		}
		return Result{}, err
	}

	transcript := joinFinalSegments(stream.Finals)
	if transcript == "" {
		transcript = strings.TrimSpace(stream.TextFinal.Text)
	}

	return Result{
		Partials:      append([]protocol.ASRPartial(nil), stream.Partials...),
		FinalSegments: append([]protocol.ASRFinal(nil), stream.Finals...),
		Transcript:    transcript,
	}, nil
}

func websocketURLFor(host string, port int) string {
	return fmt.Sprintf("ws://%s:%d/ws", host, port)
}

func mustExistPath(path string) error {
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Clean(resolved)
	}
	if _, err := os.Stat(resolved); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("must exist at %s", resolved)
		}
		return fmt.Errorf("cannot access %s: %w", resolved, err)
	}
	return nil
}

func isLocalHost(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "127.0.0.1" || trimmed == "localhost" || trimmed == "::1" {
		return true
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && ip.IsLoopback()
}

func isRuntimeUnavailable(err error) bool {
	if protocol.ErrorCodeOf(err) == protocol.ErrorCodeSidecarUnavailable {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection refused") || strings.Contains(text, "broken pipe") || strings.Contains(text, "reset by peer") || strings.Contains(text, "websocket handshake failed")
}

func isExitInterrupt(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}
