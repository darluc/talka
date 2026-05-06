package asr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type UpstreamRuntimeManagerConfig struct {
	RuntimePath    string
	RuntimeArgs    []string
	Host           string
	Port           int
	HotwordPath    string
	Models         ModelPaths
	Env            []string
	StartupTimeout time.Duration
	StopTimeout    time.Duration
	HealthTimeout  time.Duration
	Stdout         io.Writer
	Stderr         io.Writer
}

type UpstreamRuntimeManager struct {
	config UpstreamRuntimeManagerConfig

	mu  sync.Mutex
	cmd *exec.Cmd
	url string

	currentPort  int
	lastError    string
	stdoutBuffer *boundedLogBuffer
	stderrBuffer *boundedLogBuffer
}

func NewUpstreamRuntimeManager(config UpstreamRuntimeManagerConfig) *UpstreamRuntimeManager {
	if config.StartupTimeout <= 0 {
		config.StartupTimeout = defaultStartupTimeout
	}
	if config.StopTimeout <= 0 {
		config.StopTimeout = defaultStopTimeout
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = defaultHealthTimeout
	}
	return &UpstreamRuntimeManager{
		config:       config,
		url:          fmt.Sprintf("ws://%s:%d", config.Host, config.Port),
		currentPort:  config.Port,
		stdoutBuffer: newBoundedLogBuffer(runtimeLogBufferLimit),
		stderrBuffer: newBoundedLogBuffer(runtimeLogBufferLimit),
	}
}

func (cfg UpstreamRuntimeManagerConfig) Validate() error {
	shared := RuntimeManagerConfig{
		RuntimePath: cfg.RuntimePath,
		Host:        cfg.Host,
		Port:        cfg.Port,
		Models:      cfg.Models,
	}
	if err := shared.Validate(); err != nil {
		return newRuntimeError(ErrorCodeRuntimeConfigInvalid, "embedded FunASR configuration is invalid", err)
	}
	return nil
}

func (m *UpstreamRuntimeManager) URL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.url
}

func (m *UpstreamRuntimeManager) StartupTimeout() time.Duration {
	return m.config.StartupTimeout
}

func (m *UpstreamRuntimeManager) EnsureRunning(ctx context.Context) error {
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

func (m *UpstreamRuntimeManager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}

func (m *UpstreamRuntimeManager) Start(ctx context.Context) error {
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
	attemptedPorts := map[int]struct{}{}
	var lastErr error
	for attempt := 0; attempt < maxDynamicPortAttempts; attempt++ {
		launchPort, err := nextAvailableLoopbackPort(m.config.Host, m.config.Port, attemptedPorts)
		if err != nil {
			m.mu.Unlock()
			chooseErr := newRuntimeErrorWithDiagnostic(
				ErrorCodeRuntimeStartupFailed,
				"embedded FunASR runtime could not choose a local port",
				err.Error(),
				err,
			)
			m.recordLastErrorLocked(chooseErr.Error())
			return chooseErr
		}
		attemptedPorts[launchPort] = struct{}{}
		m.mu.Unlock()

		lastErr = m.startOnPort(ctx, launchPort)
		if lastErr == nil {
			m.recordLastError("")
			return nil
		}
		if !shouldRetryOnDifferentPort(lastErr) {
			m.recordLastError(lastErr.Error())
			return lastErr
		}
		m.recordLastError(lastErr.Error())
		m.mu.Lock()
		if m.cmd != nil {
			m.mu.Unlock()
			_ = m.Stop(context.Background())
			m.mu.Lock()
		}
	}
	m.mu.Unlock()

	if lastErr != nil {
		return lastErr
	}
	unknownErr := newRuntimeError(ErrorCodeRuntimeStartupFailed, "embedded FunASR runtime could not start", nil)
	m.recordLastError(unknownErr.Error())
	return unknownErr
}

func (m *UpstreamRuntimeManager) Stop(ctx context.Context) error {
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

func (m *UpstreamRuntimeManager) waitForExit(cmd *exec.Cmd) {
	_ = cmd.Wait()
	m.clearCommand(cmd)
}

func (m *UpstreamRuntimeManager) clearCommand(cmd *exec.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == cmd {
		m.cmd = nil
	}
}

func (m *UpstreamRuntimeManager) healthCheck(ctx context.Context) error {
	conn, err := dialFunASRWebSocket(ctx, m.URL(), m.config.HealthTimeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (m *UpstreamRuntimeManager) recordLastError(value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = value
}

func (m *UpstreamRuntimeManager) recordLastErrorLocked(value string) {
	m.lastError = value
}

func multiWriterOrDiscard(primary io.Writer, secondary io.Writer) io.Writer {
	switch {
	case primary == nil && secondary == nil:
		return io.Discard
	case primary == nil:
		return secondary
	case secondary == nil:
		return primary
	default:
		return io.MultiWriter(primary, secondary)
	}
}

func (m *UpstreamRuntimeManager) startOnPort(ctx context.Context, launchPort int) error {
	args := overrideFlagValue(m.config.RuntimeArgs, "--listen-ip", m.config.Host)
	args = overrideFlagValue(args, "--port", strconv.Itoa(launchPort))
	url := fmt.Sprintf("ws://%s:%d", m.config.Host, launchPort)

	m.mu.Lock()
	m.stdoutBuffer.Reset()
	m.stderrBuffer.Reset()
	cmd := exec.Command(m.config.RuntimePath, args...)
	cmd.Env = append(os.Environ(), m.config.Env...)
	cmd.Stdout = multiWriterOrDiscard(m.config.Stdout, m.stdoutBuffer)
	cmd.Stderr = multiWriterOrDiscard(m.config.Stderr, m.stderrBuffer)
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return newRuntimeErrorWithDiagnostic(
			ErrorCodeRuntimeStartupFailed,
			"embedded FunASR runtime process could not start",
			err.Error(),
			err,
		)
	}
	m.cmd = cmd
	m.currentPort = launchPort
	m.url = url
	m.lastError = ""
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
	return classifyScopedRuntimeStartupFailure(
		"embedded FunASR runtime",
		url,
		m.stderrBuffer.String(),
		fmt.Errorf("runtime did not become healthy within %s", m.config.StartupTimeout),
	)
}
