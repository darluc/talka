package asr

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultContainerImage = "registry.cn-hangzhou.aliyuncs.com/funasr_repo/funasr:funasr-runtime-sdk-online-cpu-0.1.13"

type DockerRuntimeManagerConfig struct {
	Host           string
	Port           int
	Mode           string
	Image          string
	ContainerName  string
	DownloadDir    string
	HotwordPath    string
	Models         ModelPaths
	StartupTimeout time.Duration
}

type DockerRuntimeManager struct {
	config DockerRuntimeManagerConfig

	mu          sync.Mutex
	url         string
	currentPort int
	lastError   string
}

func NewDockerRuntimeManager(config DockerRuntimeManagerConfig) *DockerRuntimeManager {
	if config.StartupTimeout <= 0 {
		config.StartupTimeout = 45 * time.Second
	}
	if strings.TrimSpace(config.Image) == "" {
		config.Image = defaultContainerImage
	}
	if strings.TrimSpace(config.Mode) == "" {
		config.Mode = "2pass"
	}
	return &DockerRuntimeManager{
		config:      config,
		url:         fmt.Sprintf("ws://%s:%d", config.Host, config.Port),
		currentPort: config.Port,
	}
}

func (m *DockerRuntimeManager) EnsureRunning(ctx context.Context) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(m.config.DownloadDir, 0o755); err != nil {
		return fmt.Errorf("create model download dir: %w", err)
	}
	if err := m.ensureHealthyContainer(ctx); err != nil {
		m.recordLastError(err.Error())
		return err
	}
	m.recordLastError("")
	return nil
}

func (m *DockerRuntimeManager) Validate() error {
	if !isLocalHost(m.config.Host) {
		return fmt.Errorf("host must stay localhost-only, got %q", m.config.Host)
	}
	if m.config.Port <= 0 {
		return fmt.Errorf("port must be greater than zero (%d)", m.config.Port)
	}
	if strings.TrimSpace(m.config.ContainerName) == "" {
		return fmt.Errorf("container_name must not be empty")
	}
	if strings.TrimSpace(m.config.DownloadDir) == "" {
		return fmt.Errorf("download_dir must not be empty")
	}
	for field, value := range map[string]string{
		"models.asr":    m.config.Models.ASR,
		"models.online": m.config.Models.Online,
		"models.vad":    m.config.Models.VAD,
		"models.punc":   m.config.Models.Punc,
		"models.itn":    m.config.Models.ITN,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be empty", field)
		}
	}
	if strings.TrimSpace(m.config.Models.LM) != "" && strings.TrimSpace(m.config.DownloadDir) == "" {
		return fmt.Errorf("download_dir must not be empty when models.lm is configured")
	}
	return nil
}

func (m *DockerRuntimeManager) URL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.url
}

func (m *DockerRuntimeManager) StartupTimeout() time.Duration {
	return m.config.StartupTimeout
}

func (m *DockerRuntimeManager) healthCheck(ctx context.Context) error {
	conn, err := dialFunASRWebSocket(ctx, m.URL(), 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (m *DockerRuntimeManager) ensureContainer(ctx context.Context) error {
	return m.ensureHealthyContainer(ctx)
}

func (m *DockerRuntimeManager) isContainerRunning(ctx context.Context) (bool, error) {
	out, err := dockerCommandOutput(ctx, "inspect", "-f", "{{.State.Running}}", m.config.ContainerName)
	if err != nil {
		if strings.Contains(err.Error(), "No such object") {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

func (m *DockerRuntimeManager) containerExists(ctx context.Context) (bool, error) {
	out, err := dockerCommandOutput(ctx, "inspect", "-f", "{{.Name}}", m.config.ContainerName)
	if err != nil {
		if strings.Contains(err.Error(), "No such object") {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func dockerRunArgs(cfg DockerRuntimeManagerConfig, hostPort int) ([]string, error) {
	publishedPort := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", hostPort))
	downloadDir, err := filepath.Abs(cfg.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("resolve download_dir: %w", err)
	}

	runtimeArgs := []string{
		"/workspace/FunASR/runtime/websocket/build/bin/funasr-wss-server-2pass",
		"--download-model-dir", "/workspace/models",
		"--model-dir", cfg.Models.ASR,
		"--online-model-dir", cfg.Models.Online,
		"--vad-dir", cfg.Models.VAD,
		"--punc-dir", cfg.Models.Punc,
		"--itn-dir", cfg.Models.ITN,
		"--lm-dir", dockerLMDir(cfg.Models.LM),
		"--hotword", cfg.HotwordPath,
		"--port", "10095",
		"--listen-ip", "0.0.0.0",
		"--certfile", "0",
	}

	args := []string{
		"run", "-d",
		"--name", cfg.ContainerName,
		"-p", publishedPort + ":10095",
		"-v", downloadDir + ":/workspace/models",
		cfg.Image,
	}
	args = append(args, runtimeArgs...)
	return args, nil
}

func dockerLMDir(path string) string {
	if strings.TrimSpace(path) == "" {
		return "none"
	}
	return path
}

func dockerCommandOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func runDockerCommand(ctx context.Context, args ...string) error {
	_, err := dockerCommandOutput(ctx, args...)
	return err
}

func (m *DockerRuntimeManager) ensureHealthyContainer(ctx context.Context) error {
	running, err := m.isContainerRunning(ctx)
	if err != nil {
		return err
	}
	if running {
		if port, err := m.runningHostPort(ctx); err == nil {
			m.setURL(port)
			if err := m.healthCheck(ctx); err == nil {
				return nil
			}
		}
		if err := m.removeContainer(ctx); err != nil {
			return err
		}
	} else {
		exists, err := m.containerExists(ctx)
		if err != nil {
			return err
		}
		if exists {
			if err := m.removeContainer(ctx); err != nil {
				return err
			}
		}
	}

	attemptedPorts := map[int]struct{}{}
	var lastErr error
	for attempt := 0; attempt < maxDynamicPortAttempts; attempt++ {
		launchPort, err := nextAvailableLoopbackPort(m.config.Host, m.config.Port, attemptedPorts)
		if err != nil {
			return newRuntimeErrorWithDiagnostic(
				ErrorCodeRuntimeStartupFailed,
				"managed FunASR container could not choose a local port",
				err.Error(),
				err,
			)
		}
		attemptedPorts[launchPort] = struct{}{}

		if err := m.runFreshContainer(ctx, launchPort); err != nil {
			lastErr = classifyScopedRuntimeStartupFailure(
				"managed FunASR container",
				fmt.Sprintf("ws://%s:%d", m.config.Host, launchPort),
				err.Error(),
				err,
			)
			if shouldRetryOnDifferentPort(lastErr) {
				_ = m.removeContainer(context.Background())
				continue
			}
			return lastErr
		}

		m.setURL(launchPort)
		if err := m.waitForHealth(ctx); err != nil {
			lastErr = err
			_ = m.removeContainer(context.Background())
			if shouldRetryOnDifferentPort(err) {
				continue
			}
			return err
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return newRuntimeError(ErrorCodeRuntimeStartupFailed, "managed FunASR container could not start", nil)
}

func (m *DockerRuntimeManager) runFreshContainer(ctx context.Context, hostPort int) error {
	args, err := dockerRunArgs(m.config, hostPort)
	if err != nil {
		return err
	}
	return runDockerCommand(ctx, args...)
}

func (m *DockerRuntimeManager) waitForHealth(ctx context.Context) error {
	deadline := time.Now().Add(m.config.StartupTimeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.healthCheck(ctx); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	logs := m.containerLogs(ctx)
	return classifyScopedRuntimeStartupFailure(
		"managed FunASR container",
		m.URL(),
		logs,
		fmt.Errorf("runtime did not become healthy within %s", m.config.StartupTimeout),
	)
}

func (m *DockerRuntimeManager) runningHostPort(ctx context.Context) (int, error) {
	out, err := dockerCommandOutput(ctx, "port", m.config.ContainerName, "10095/tcp")
	if err != nil {
		return 0, err
	}
	return parseDockerPublishedPort(out)
}

func parseDockerPublishedPort(output string) (int, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lastColon := strings.LastIndex(line, ":")
		if lastColon < 0 || lastColon+1 >= len(line) {
			continue
		}
		portText := strings.TrimSpace(line[lastColon+1:])
		port, err := strconv.Atoi(portText)
		if err == nil && port > 0 {
			return port, nil
		}
	}
	return 0, fmt.Errorf("could not parse docker published port from %q", strings.TrimSpace(output))
}

func (m *DockerRuntimeManager) removeContainer(ctx context.Context) error {
	err := runDockerCommand(ctx, "rm", "-f", m.config.ContainerName)
	if err != nil && strings.Contains(err.Error(), "No such container") {
		return nil
	}
	return err
}

func (m *DockerRuntimeManager) containerLogs(ctx context.Context) string {
	out, err := dockerCommandOutput(ctx, "logs", m.config.ContainerName)
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(out)
}

func (m *DockerRuntimeManager) setURL(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentPort = port
	m.url = fmt.Sprintf("ws://%s:%d", m.config.Host, port)
}

func (m *DockerRuntimeManager) recordLastError(value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = value
}
