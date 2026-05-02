package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"talka/internal/config"
	"talka/internal/pairing"
)

const (
	statusRunning            = "running"
	permissionAccessibility  = "accessibility"
	accessibilitySettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	pairingLifetime          = 2 * time.Minute
)

var newIOSPairingStore = func() pairing.Store { return pairing.NewDarwinStore() }

type App struct {
	mu          sync.RWMutex
	cfg         config.Config
	configPath  string
	configDir   string
	startedAt   time.Time
	logger      *slog.Logger
	devices     map[string]Device
	pairing     *PairingSession
	pipeline    *Pipeline
	iosSessions map[string]*iosAudioSession
	iosPairing  *pairing.StartResponse
	iosManager  *pairing.Manager
}

type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Paired     bool      `json:"paired"`
	LastSeenAt time.Time `json:"last_seen_at,omitzero"`
}

type PairingSession struct {
	PairingID string    `json:"pairing_id"`
	PIN       string    `json:"pin"`
	ExpiresAt time.Time `json:"expires_at"`
}

type StatusResponse struct {
	ServiceName   string                    `json:"service_name"`
	State         string                    `json:"state"`
	ConfigPath    string                    `json:"config_path"`
	UptimeSeconds int64                     `json:"uptime_seconds"`
	DeviceCount   int                       `json:"device_count"`
	PairingActive bool                      `json:"pairing_active"`
	ASR           ASRStatusResponse         `json:"asr"`
	Ollama        OllamaStatusResponse      `json:"ollama"`
	Permissions   PermissionsStatusResponse `json:"permissions"`
}

type ASRStatusResponse struct {
	Provider    string `json:"provider"`
	RuntimePath string `json:"runtime_path"`
	SampleRate  int    `json:"sample_rate"`
	Mode        string `json:"mode"`
}

type OllamaStatusResponse struct {
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type PermissionsStatusResponse struct {
	Accessibility string `json:"accessibility"`
}

type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

type PairingStartResponse struct {
	PairingID        string    `json:"pairing_id"`
	PIN              string    `json:"pin"`
	ExpiresAt        time.Time `json:"expires_at"`
	ExpiresInSeconds int       `json:"expires_in_seconds"`
}

type ForgetDeviceResponse struct {
	DeviceID  string `json:"device_id"`
	Forgotten bool   `json:"forgotten"`
}

type ConfigResponse struct {
	Path   string        `json:"path"`
	Config config.Config `json:"config"`
}

type AccessibilityOpenResponse struct {
	Permission  string `json:"permission"`
	Opened      bool   `json:"opened"`
	SettingsURL string `json:"settings_url"`
	Message     string `json:"message"`
}

type IOSPairingRequest struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	PIN        string `json:"pin"`
}

type IOSPairingChallengeResponse struct {
	PairingID                string    `json:"pairing_id"`
	ExpiresAt                time.Time `json:"expires_at"`
	ServerDeviceID           string    `json:"server_device_id"`
	ServerDeviceName         string    `json:"server_device_name"`
	ServerIdentityPublicKey  string    `json:"server_identity_public_key"`
	ServerEphemeralPublicKey string    `json:"server_ephemeral_public_key"`
}

type IOSPairingCompleteRequest struct {
	PairingID                string `json:"pairing_id"`
	DeviceID                 string `json:"device_id"`
	DeviceName               string `json:"device_name"`
	ClientIdentityPublicKey  string `json:"client_identity_public_key"`
	ClientEphemeralPublicKey string `json:"client_ephemeral_public_key"`
	ClientConfirmation       string `json:"client_confirmation"`
}

type IOSResumeRequest struct {
	PairingID                string `json:"pairing_id"`
	DeviceID                 string `json:"device_id"`
	DeviceName               string `json:"device_name"`
	ClientIdentityPublicKey  string `json:"client_identity_public_key"`
	ClientEphemeralPublicKey string `json:"client_ephemeral_public_key"`
}

type IOSPairingResponse struct {
	DeviceID                 string `json:"device_id"`
	DeviceName               string `json:"device_name"`
	ServerDeviceID           string `json:"server_device_id"`
	ServerDeviceName         string `json:"server_device_name"`
	ServerIdentityPublicKey  string `json:"server_identity_public_key"`
	ServerEphemeralPublicKey string `json:"server_ephemeral_public_key"`
	ServerConfirmation       string `json:"server_confirmation"`
	SessionID                string `json:"session_id"`
	AudioWebSocketURL        string `json:"audio_websocket_url"`
}

type DiagnosticsExportResponse struct {
	Config     config.Config              `json:"config"`
	Devices    []Device                   `json:"devices"`
	Pairing    *DiagnosticsPairingSession `json:"pairing,omitempty"`
	Redactions []string                   `json:"redactions"`
}

type DiagnosticsPairingSession struct {
	PairingID string    `json:"pairing_id"`
	PIN       string    `json:"pin"`
	ExpiresAt time.Time `json:"expires_at"`
}

type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}

func New(cfg config.Config, configPath string, logger *slog.Logger) (*App, error) {
	pipeline, err := newPipelineFromConfig(cfg, filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	return NewWithPipeline(cfg, configPath, logger, pipeline)
}

func NewWithPipeline(cfg config.Config, configPath string, logger *slog.Logger, pipeline *Pipeline) (*App, error) {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}

	configDir := filepath.Dir(configPath)
	if err := cfg.Validate(configDir); err != nil {
		return nil, err
	}

	app := &App{
		cfg:         cfg,
		configPath:  configPath,
		configDir:   configDir,
		startedAt:   time.Now(),
		logger:      logger,
		devices:     map[string]Device{},
		pipeline:    pipeline,
		iosSessions: map[string]*iosAudioSession{},
	}
	app.iosManager = pairing.NewManager(pairing.Config{ServerDeviceID: "talka-mac", ServerDeviceName: cfg.Server.ServiceName, Store: newIOSPairingStore(), PairingTTL: pairingLifetime, InactivityTimeout: 30 * time.Second})
	app.logger.Info("app started",
		"service_name", cfg.Server.ServiceName,
		"bind_host", cfg.Server.BindHost,
		"port", cfg.Server.Port,
		"config_path", configPath,
		"logging_level", cfg.Logging.Level,
		"capture_audio", cfg.Logging.CaptureAudio,
		"capture_transcript", cfg.Logging.CaptureTranscript,
	)
	return app, nil
}

func NewLogger(w io.Writer, level string) *slog.Logger {
	opts := &slog.HandlerOptions{}
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", a.handleStatus)
	mux.HandleFunc("/v1/devices", a.handleDevices)
	mux.HandleFunc("/v1/pairing/start", a.handlePairingStart)
	mux.HandleFunc("/v1/ios/pairing/challenge", a.handleIOSPairingChallenge)
	mux.HandleFunc("/v1/ios/resume", a.handleIOSResume)
	mux.HandleFunc("/v1/config", a.handleConfig)
	mux.HandleFunc("/v1/diagnostics/export", a.handleDiagnosticsExport)
	mux.HandleFunc("/v1/permissions/accessibility/open", a.handleAccessibilityOpen)
	mux.HandleFunc("/v1/ios/pair", a.handleIOSPair)
	mux.HandleFunc("/v1/session/audio", a.handleIOSAudioStream)
	mux.HandleFunc("/v1/devices/", a.handleDeviceActions)
	return mux
}

func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	server := &http.Server{Handler: a.Handler()}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		err := <-serverErr
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "status endpoint only accepts GET", nil)
		return
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	writeJSON(w, http.StatusOK, StatusResponse{
		ServiceName:   a.cfg.Server.ServiceName,
		State:         statusRunning,
		ConfigPath:    a.configPath,
		UptimeSeconds: int64(time.Since(a.startedAt).Round(time.Second) / time.Second),
		DeviceCount:   len(a.devices),
		PairingActive: a.pairing != nil && time.Now().Before(a.pairing.ExpiresAt),
		ASR:           ASRStatusResponse{Provider: a.cfg.ASR.Provider, RuntimePath: a.cfg.ASR.RuntimePath, SampleRate: a.cfg.ASR.SampleRate, Mode: a.cfg.ASR.Mode},
		Ollama:        OllamaStatusResponse{BaseURL: a.cfg.LLM.BaseURL, Model: a.cfg.LLM.Model, TimeoutSeconds: a.cfg.LLM.TimeoutSeconds},
		Permissions:   PermissionsStatusResponse{Accessibility: "unknown"},
	})
}

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "devices endpoint only accepts GET", nil)
		return
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	devices := make([]Device, 0, len(a.devices))
	for _, device := range a.devices {
		devices = append(devices, device)
	}
	writeJSON(w, http.StatusOK, DevicesResponse{Devices: devices})
}

func (a *App) handlePairingStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "pairing start endpoint only accepts POST", nil)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	iosPairing, err := a.iosManager.Start()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pairing_start_failed", "pairing start failed", nil)
		return
	}
	session := &PairingSession{PairingID: iosPairing.PairingID, PIN: iosPairing.PIN, ExpiresAt: iosPairing.ExpiresAt}
	a.pairing = session
	a.iosPairing = &iosPairing
	a.logger.Info("pairing started", "pairing_id", session.PairingID, "expires_in_seconds", int(pairingLifetime/time.Second))
	writeJSON(w, http.StatusOK, PairingStartResponse{
		PairingID:        session.PairingID,
		PIN:              session.PIN,
		ExpiresAt:        session.ExpiresAt,
		ExpiresInSeconds: int(pairingLifetime / time.Second),
	})
}

func (a *App) handleDeviceActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/devices/")
	if !strings.HasSuffix(path, "/forget") {
		writeError(w, http.StatusNotFound, "not_found", "unknown device action", nil)
		return
	}
	deviceID := strings.TrimSuffix(path, "/forget")
	deviceID = strings.TrimSuffix(deviceID, "/")
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "device id is required", nil)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "forget endpoint only accepts POST", nil)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.devices, deviceID)
	a.logger.Info("device forgotten", "device_id", deviceID)
	writeJSON(w, http.StatusOK, ForgetDeviceResponse{DeviceID: deviceID, Forgotten: true})
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.RLock()
		defer a.mu.RUnlock()
		writeJSON(w, http.StatusOK, ConfigResponse{Path: a.configPath, Config: a.cfg})
	case http.MethodPut:
		var updated config.Config
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			a.logger.Error("failed to decode config payload", "error", err)
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid config payload", nil)
			return
		}
		if err := updated.Validate(a.configDir); err != nil {
			a.logger.Error("invalid config", "error", err)
			writeConfigError(w, err)
			return
		}
		if err := config.Save(a.configPath, updated); err != nil {
			a.logger.Error("failed to write config", "error", err)
			writeError(w, http.StatusInternalServerError, "config_write_failed", "failed to write config", nil)
			return
		}
		a.mu.Lock()
		a.cfg = updated
		a.mu.Unlock()
		a.logger.Info("config updated", "config_path", a.configPath, "logging_level", updated.Logging.Level)
		writeJSON(w, http.StatusOK, ConfigResponse{Path: a.configPath, Config: updated})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "config endpoint only accepts GET or PUT", nil)
	}
}

func (a *App) handleAccessibilityOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "accessibility endpoint only accepts POST", nil)
		return
	}
	writeJSON(w, http.StatusOK, AccessibilityOpenResponse{
		Permission:  permissionAccessibility,
		Opened:      false,
		SettingsURL: accessibilitySettingsURL,
		Message:     "Open System Settings → Privacy & Security → Accessibility",
	})
}

func (a *App) handleDiagnosticsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "diagnostics export endpoint only accepts GET", nil)
		return
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	devices := make([]Device, 0, len(a.devices))
	for _, device := range a.devices {
		devices = append(devices, device)
	}

	var pairing *DiagnosticsPairingSession
	if a.pairing != nil {
		pairing = &DiagnosticsPairingSession{PairingID: a.pairing.PairingID, PIN: "[redacted]", ExpiresAt: a.pairing.ExpiresAt}
	}

	writeJSON(w, http.StatusOK, DiagnosticsExportResponse{
		Config:     a.cfg,
		Devices:    devices,
		Pairing:    pairing,
		Redactions: diagnosticsRedactions(a.cfg),
	})
}

func diagnosticsRedactions(cfg config.Config) []string {
	redactions := []string{"pairing.pin", "session.keys"}
	if !cfg.Logging.CaptureAudio {
		redactions = append(redactions, "raw_audio")
	}
	if !cfg.Logging.CaptureTranscript {
		redactions = append(redactions, "full_transcript")
	}
	return redactions
}

func writeConfigError(w http.ResponseWriter, err error) {
	validationErr, ok := err.(config.ValidationError)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_config", "configuration is invalid", nil)
		return
	}
	issues := make([]map[string]any, 0, len(validationErr.Issues))
	for _, issue := range validationErr.Issues {
		issues = append(issues, map[string]any{"field": issue.Field, "value": issue.Value, "problem": issue.Problem})
	}
	writeError(w, http.StatusBadRequest, "invalid_config", "configuration is invalid", map[string]any{"issues": issues})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, ErrorResponse{Error: APIError{Code: code, Message: message, Details: details}})
}
