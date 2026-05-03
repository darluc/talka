package app

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"talka/internal/asr"
	"talka/internal/config"
	intcrypto "talka/internal/crypto"
	"talka/internal/inject"
	"talka/internal/llm"
	"talka/internal/pairing"
	"talka/internal/protocol"
	"talka/internal/session"
)

func TestStatusEndpointReturnsTypedJSON(t *testing.T) {
	server := newTestServer(t)

	resp := mustGet(t, server.URL+"/v1/status")
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var payload StatusResponse
	decodeJSON(t, resp, &payload)

	if payload.ServiceName != "Talka" {
		t.Fatalf("ServiceName = %q, want %q", payload.ServiceName, "Talka")
	}
	if payload.State != "running" {
		t.Fatalf("State = %q, want running", payload.State)
	}
	if payload.DeviceCount != 0 {
		t.Fatalf("DeviceCount = %d, want 0", payload.DeviceCount)
	}
	if payload.ASR.Provider != "funasr_onnx" || payload.ASR.SampleRate != 16000 {
		t.Fatalf("ASR status = %+v, want provider funasr_onnx and sample_rate 16000", payload.ASR)
	}
	if payload.Ollama.BaseURL != "http://localhost:11434" || payload.Ollama.Model != "qwen3:8b" {
		t.Fatalf("Ollama status = %+v, want default endpoint/model", payload.Ollama)
	}
	if payload.Permissions.Accessibility != "unknown" {
		t.Fatalf("Accessibility permission = %q, want unknown", payload.Permissions.Accessibility)
	}
}

func TestDevicesEndpointReturnsTypedList(t *testing.T) {
	server := newTestServer(t)

	resp := mustGet(t, server.URL+"/v1/devices")
	defer resp.Body.Close()

	var payload DevicesResponse
	decodeJSON(t, resp, &payload)

	if len(payload.Devices) != 0 {
		t.Fatalf("len(Devices) = %d, want 0", len(payload.Devices))
	}
}

func TestPairingStartReturnsPINAndExpiresAt(t *testing.T) {
	server := newTestServer(t)

	resp := mustPost(t, server.URL+"/v1/pairing/start", nil)
	defer resp.Body.Close()

	var payload PairingStartResponse
	decodeJSON(t, resp, &payload)

	if len(payload.PairingID) == 0 {
		t.Fatal("PairingID is empty")
	}
	if len(payload.PIN) != 6 {
		t.Fatalf("PIN = %q, want six digits", payload.PIN)
	}
	if payload.ExpiresInSeconds != 120 {
		t.Fatalf("ExpiresInSeconds = %d, want 120", payload.ExpiresInSeconds)
	}
	if payload.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt is zero")
	}
}

func TestForgetDeviceReturnsTypedAcknowledgement(t *testing.T) {
	server := newTestServer(t)

	resp := mustPost(t, server.URL+"/v1/devices/device-123/forget", nil)
	defer resp.Body.Close()

	var payload ForgetDeviceResponse
	decodeJSON(t, resp, &payload)

	if payload.DeviceID != "device-123" {
		t.Fatalf("DeviceID = %q, want device-123", payload.DeviceID)
	}
	if !payload.Forgotten {
		t.Fatal("Forgotten = false, want true")
	}
}

func TestConfigEndpointPersistsTypedConfig(t *testing.T) {
	server := newWritableTestServer(t)

	resp := mustGet(t, server.URL+"/v1/config")
	defer resp.Body.Close()

	var got ConfigResponse
	decodeJSON(t, resp, &got)
	if got.Path == "" {
		t.Fatal("Path is empty")
	}
	if got.Config.Server.ServiceName != "Talka" {
		t.Fatalf("Config.Server.ServiceName = %q, want %q", got.Config.Server.ServiceName, "Talka")
	}

	updated := got.Config
	updated.Logging.Level = "debug"
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("Marshal(updated) error = %v", err)
	}

	putReq, err := http.NewRequest(http.MethodPut, server.URL+"/v1/config", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT /v1/config error = %v", err)
	}
	defer putResp.Body.Close()

	var putPayload ConfigResponse
	decodeJSON(t, putResp, &putPayload)
	if putPayload.Config.Logging.Level != "debug" {
		t.Fatalf("Config.Logging.Level = %q, want debug", putPayload.Config.Logging.Level)
	}

	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", got.Path, err)
	}
	if !strings.Contains(string(data), "level: debug") {
		t.Fatalf("saved config = %q, want debug persisted", string(data))
	}
}

func TestAccessibilityOpenReturnsActionableGuidance(t *testing.T) {
	server := newTestServer(t)

	resp := mustPost(t, server.URL+"/v1/permissions/accessibility/open", nil)
	defer resp.Body.Close()

	var payload AccessibilityOpenResponse
	decodeJSON(t, resp, &payload)
	if payload.Permission != "accessibility" {
		t.Fatalf("Permission = %q, want accessibility", payload.Permission)
	}
	if payload.SettingsURL == "" {
		t.Fatal("SettingsURL is empty")
	}
}

func TestConfigEndpointRejectsInvalidConfig(t *testing.T) {
	server := newWritableTestServer(t)

	bad := `{"asr":{"runtime_path":"runtime","port":10095,"models":{"asr":"models/asr","vad":"models/missing","punc":"models/punc","itn":"models/itn"}}}`
	resp := mustPut(t, server.URL+"/v1/config", strings.NewReader(bad))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", resp.StatusCode)
	}

	var payload ErrorResponse
	decodeJSON(t, resp, &payload)
	if payload.Error.Code != "invalid_config" {
		t.Fatalf("Error.Code = %q, want invalid_config", payload.Error.Code)
	}
}

func TestDiagnosticsExportRedactsPINsAndPrivateCaptureByDefault(t *testing.T) {
	server := newTestServer(t)

	pairingResp := mustPost(t, server.URL+"/v1/pairing/start", nil)
	var pairing PairingStartResponse
	decodeJSON(t, pairingResp, &pairing)
	pairingResp.Body.Close()

	resp := mustGet(t, server.URL+"/v1/diagnostics/export")
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(diagnostics) error = %v", err)
	}
	body := string(data)
	if strings.Contains(body, pairing.PIN) {
		t.Fatalf("diagnostics export leaked PIN %q: %s", pairing.PIN, body)
	}
	if !strings.Contains(body, `"pin":"[redacted]"`) {
		t.Fatalf("diagnostics export = %s, want redacted PIN marker", body)
	}
	if !strings.Contains(body, `"raw_audio"`) || !strings.Contains(body, `"full_transcript"`) {
		t.Fatalf("diagnostics export = %s, want private capture redaction markers", body)
	}
}

func TestIOSPairingRejectsLegacyPINPayloadWithoutExposingRawSessionKeys(t *testing.T) {
	server := newTestServer(t)

	pairingResp := mustPost(t, server.URL+"/v1/pairing/start", nil)
	var pairing PairingStartResponse
	decodeJSON(t, pairingResp, &pairing)
	pairingResp.Body.Close()

	completeBody := strings.NewReader(`{"device_id":"iphone-1","device_name":"ZVVZ","pin":"` + pairing.PIN + `"}`)
	completeResp := mustPost(t, server.URL+"/v1/ios/pair", completeBody)
	defer completeResp.Body.Close()

	data, err := io.ReadAll(completeResp.Body)
	if err != nil {
		t.Fatalf("ReadAll(/v1/ios/pair) error = %v", err)
	}
	if completeResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, body = %s, want 400 for legacy payload", completeResp.StatusCode, string(data))
	}
	body := string(data)
	for _, forbidden := range []string{"client_to_server_key", "server_to_client_key"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("/v1/ios/pair leaked %s in response body: %s", forbidden, body)
		}
	}
}

func TestIOSSecurePairingDoesNotExposeRawSessionKeysAndProcessesEncryptedAudio(t *testing.T) {
	cfg, cfgPath := mustConfig(t)
	runtime := &asr.FakeRuntime{Ready: true}
	runtimeServer := httptest.NewServer(runtime.Handler())
	defer runtimeServer.Close()

	service, err := NewWithPipeline(cfg, cfgPath, nil, NewPipeline(
		asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(runtimeServer.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second}),
		llm.NewFakeProvider(llm.FakeConfig{}),
		inject.NewFakeInjector(),
	))
	if err != nil {
		t.Fatalf("NewWithPipeline() error = %v", err)
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	startResp := mustPost(t, server.URL+"/v1/pairing/start", nil)
	var start PairingStartResponse
	decodeJSON(t, startResp, &start)
	startResp.Body.Close()

	challengeResp := mustGet(t, server.URL+"/v1/ios/pairing/challenge")
	var challenge iosPairingChallengeTestResponse
	decodeJSON(t, challengeResp, &challenge)
	challengeResp.Body.Close()
	if challenge.PairingID != start.PairingID {
		t.Fatalf("challenge PairingID = %q, want %q", challenge.PairingID, start.PairingID)
	}

	clientIdentity, clientEphemeral := mustClientKeys(t)
	confirmation := mustAppPairingConfirmation(t, challenge, clientIdentity, clientEphemeral, "iphone-1", "ZVVZ", start.PIN)
	pairBody := marshalJSONReader(t, iosPairingCompleteTestRequest{PairingID: challenge.PairingID, DeviceID: "iphone-1", DeviceName: "ZVVZ", ClientIdentityPublicKey: base64.StdEncoding.EncodeToString(clientIdentity.Public), ClientEphemeralPublicKey: base64.StdEncoding.EncodeToString(clientEphemeral.Public), ClientConfirmation: base64.StdEncoding.EncodeToString(confirmation)})
	pairResp := mustPost(t, server.URL+"/v1/ios/pair", pairBody)
	defer pairResp.Body.Close()
	data, err := io.ReadAll(pairResp.Body)
	if err != nil {
		t.Fatalf("ReadAll(pair response) error = %v", err)
	}
	if pairResp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, body = %s, want 200", pairResp.StatusCode, string(data))
	}
	for _, forbidden := range []string{"client_to_server_key", "server_to_client_key"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("pair response leaked %s: %s", forbidden, string(data))
		}
	}
	var paired iosPairingTestResponse
	if err := json.Unmarshal(data, &paired); err != nil {
		t.Fatalf("Unmarshal(pair response) error = %v", err)
	}
	clientSession := mustAppClientSession(t, intcrypto.FlowPairing, challenge.PairingID, paired, clientIdentity, clientEphemeral, start.PIN)
	messages := encryptTestAudioMessages(t, clientSession, paired.SessionID)
	result, err := service.ProcessEncryptedIOSAudioSession(context.Background(), "iphone-1", messages)
	if err != nil {
		t.Fatalf("ProcessEncryptedIOSAudioSession() error = %v", err)
	}
	if got, want := result.FinalText, "你好，世界"; got != want {
		t.Fatalf("FinalText = %q, want %q", got, want)
	}
}

func TestIOSSecureResumeUsesFreshSessionID(t *testing.T) {
	server := newTestServer(t)
	start, paired, clientIdentity := pairHTTPClient(t, server.URL)

	resumeEphemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(resume ephemeral) error = %v", err)
	}
	resumeBody := marshalJSONReader(t, iosResumeTestRequest{PairingID: start.PairingID, DeviceID: "iphone-1", DeviceName: "ZVVZ", ClientIdentityPublicKey: base64.StdEncoding.EncodeToString(clientIdentity.Public), ClientEphemeralPublicKey: base64.StdEncoding.EncodeToString(resumeEphemeral.Public)})
	resumeResp := mustPost(t, server.URL+"/v1/ios/resume", resumeBody)
	defer resumeResp.Body.Close()
	var resumed iosPairingTestResponse
	decodeJSON(t, resumeResp, &resumed)
	if resumed.SessionID == paired.SessionID {
		t.Fatal("resume reused prior session id")
	}
	if resumed.ServerEphemeralPublicKey == paired.ServerEphemeralPublicKey {
		t.Fatal("resume reused prior server ephemeral public key")
	}
}

func TestIOSWebSocketErrorCodePropagatesInjectionFailures(t *testing.T) {
	err := &inject.InsertError{
		Code:        inject.FailureCodeAccessibilityMissing,
		UserMessage: "Talka needs Accessibility or Automation permission before it can paste into other apps.",
		Recovery: inject.Recovery{
			Action:     inject.RecoveryActionOpenAccessibilityGuidance,
			FailedText: "你好，世界",
			Volatile:   true,
		},
		Err: inject.ErrAccessibilityPermissionDenied,
	}

	got := iosWebSocketErrorCode(err, "process")

	if got.Code != "accessibility_missing" {
		t.Fatalf("Code = %q, want accessibility_missing", got.Code)
	}
	if got.Message != "Talka needs Accessibility or Automation permission before it can paste into other apps." {
		t.Fatalf("Message = %q, want propagated insert error message", got.Message)
	}
}

func TestProcessEncryptedIOSAudioSessionDoesNotWaitForTextInsertion(t *testing.T) {
	cfg, cfgPath := mustConfig(t)
	runtime := &asr.FakeRuntime{Ready: true}
	runtimeServer := httptest.NewServer(runtime.Handler())
	defer runtimeServer.Close()

	injector := newBlockingTestInjector()
	defer close(injector.release)
	service, err := NewWithPipeline(cfg, cfgPath, nil, NewPipeline(
		asr.NewSidecarProvider(asr.Config{URL: "ws" + strings.TrimPrefix(runtimeServer.URL, "http") + "/ws", Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second}),
		llm.NewFakeProvider(llm.FakeConfig{}),
		injector,
	))
	if err != nil {
		t.Fatalf("NewWithPipeline() error = %v", err)
	}
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	startResp := mustPost(t, server.URL+"/v1/pairing/start", nil)
	var start PairingStartResponse
	decodeJSON(t, startResp, &start)
	startResp.Body.Close()

	challengeResp := mustGet(t, server.URL+"/v1/ios/pairing/challenge")
	var challenge iosPairingChallengeTestResponse
	decodeJSON(t, challengeResp, &challenge)
	challengeResp.Body.Close()

	clientIdentity, clientEphemeral := mustClientKeys(t)
	confirmation := mustAppPairingConfirmation(t, challenge, clientIdentity, clientEphemeral, "iphone-1", "ZVVZ", start.PIN)
	pairBody := marshalJSONReader(t, iosPairingCompleteTestRequest{PairingID: challenge.PairingID, DeviceID: "iphone-1", DeviceName: "ZVVZ", ClientIdentityPublicKey: base64.StdEncoding.EncodeToString(clientIdentity.Public), ClientEphemeralPublicKey: base64.StdEncoding.EncodeToString(clientEphemeral.Public), ClientConfirmation: base64.StdEncoding.EncodeToString(confirmation)})
	pairResp := mustPost(t, server.URL+"/v1/ios/pair", pairBody)
	defer pairResp.Body.Close()

	var paired iosPairingTestResponse
	decodeJSON(t, pairResp, &paired)
	clientSession := mustAppClientSession(t, intcrypto.FlowPairing, challenge.PairingID, paired, clientIdentity, clientEphemeral, start.PIN)
	messages := encryptTestAudioMessages(t, clientSession, paired.SessionID)

	done := make(chan struct{})
	var result ProcessResult
	var processErr error
	go func() {
		result, processErr = service.ProcessEncryptedIOSAudioSession(context.Background(), "iphone-1", messages)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ProcessEncryptedIOSAudioSession() blocked on text insertion")
	}

	if processErr != nil {
		t.Fatalf("ProcessEncryptedIOSAudioSession() error = %v", processErr)
	}
	if got, want := result.FinalText, "你好，世界"; got != want {
		t.Fatalf("FinalText = %q, want %q", got, want)
	}
	if injector.calls() != 0 {
		t.Fatalf("Insert() calls = %d, want 0 while preparing iOS audio response", injector.calls())
	}
}

func TestQueueIOSFinalTextInsertionRunsInjectorInBackground(t *testing.T) {
	cfg, cfgPath := mustConfig(t)
	injector := newBlockingTestInjector()
	defer close(injector.release)
	service, err := NewWithPipeline(cfg, cfgPath, nil, &Pipeline{injector: injector})
	if err != nil {
		t.Fatalf("NewWithPipeline() error = %v", err)
	}

	service.queueIOSFinalTextInsertion("iphone-1", "你好，世界")

	select {
	case text := <-injector.started:
		if got, want := text, "你好，世界"; got != want {
			t.Fatalf("Insert() text = %q, want %q", got, want)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queueIOSFinalTextInsertion() did not start background insertion")
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	useFakeIOSPairingStore(t)
	cfg, cfgPath := mustConfig(t)
	service, err := New(cfg, cfgPath, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return httptest.NewServer(service.Handler())
}

func TestNewConfiguresProductionPipeline(t *testing.T) {
	cfg, cfgPath := mustConfig(t)
	service, err := New(cfg, cfgPath, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if service.pipeline == nil {
		t.Fatal("New() left pipeline nil; production iOS audio would fail with audio pipeline is not configured")
	}
}

func TestASRRuntimeArgsStartManagedRuntimeOnConfiguredAddress(t *testing.T) {
	cfg, _ := mustConfig(t)
	args := asrRuntimeArgsFromConfig(cfg.ASR)

	want := []string{"serve", "--addr", "127.0.0.1:10095", "--mode", "twopass"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("asrRuntimeArgsFromConfig() = %#v, want %#v", args, want)
	}
}

func newWritableTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	useFakeIOSPairingStore(t)
	cfg, cfgPath := mustConfig(t)
	service, err := New(cfg, cfgPath, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return httptest.NewServer(service.Handler())
}

func useFakeIOSPairingStore(t *testing.T) {
	t.Helper()
	previous := newIOSPairingStore
	newIOSPairingStore = func() pairing.Store { return pairing.NewFakeStore() }
	t.Cleanup(func() { newIOSPairingStore = previous })
}

func mustConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, root, "runtime")
	mustMkdir(t, root, "models/asr")
	mustMkdir(t, root, "models/vad")
	mustMkdir(t, root, "models/punc")
	mustMkdir(t, root, "models/itn")

	path := filepath.Join(root, "config.yaml")
	contents := []byte(`server:
  bind_host: 0.0.0.0
  port: 0
  service_name: Talka
asr:
  provider: funasr_onnx
  runtime_path: runtime
  host: 127.0.0.1
  port: 10095
  mode: twopass
  sample_rate: 16000
  models:
    asr: models/asr
    vad: models/vad
    punc: models/punc
    itn: models/itn
llm:
  provider: ollama
  base_url: http://localhost:11434
  model: qwen3:8b
  timeout_seconds: 30
injection:
  mode: clipboard_paste
  restore_clipboard: true
logging:
  level: info
  capture_audio: false
  capture_transcript: false
`)

	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return cfg, path
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	return resp
}

func mustPost(t *testing.T, url string, body *strings.Reader) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		reader = body
	}
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s error = %v", url, err)
	}
	return resp
}

func mustPut(t *testing.T, url string, body *strings.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s error = %v", url, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
}

func mustMkdir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", rel, err)
	}
}

func mustClientKeys(t *testing.T) (intcrypto.KeyPair, intcrypto.KeyPair) {
	t.Helper()
	identity, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(identity) error = %v", err)
	}
	ephemeral, err := intcrypto.GenerateX25519KeyPair(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateX25519KeyPair(ephemeral) error = %v", err)
	}
	return identity, ephemeral
}

func mustAppPairingConfirmation(t *testing.T, challenge iosPairingChallengeTestResponse, clientIdentity, clientEphemeral intcrypto.KeyPair, clientDeviceID, clientDeviceName, pin string) []byte {
	t.Helper()
	serverIdentityPublicKey := mustDecodeBase64(t, challenge.ServerIdentityPublicKey)
	serverEphemeralPublicKey := mustDecodeBase64(t, challenge.ServerEphemeralPublicKey)
	transcript := intcrypto.Transcript{Version: protocol.VersionV1Alpha1, Flow: intcrypto.FlowPairing, PairingID: challenge.PairingID, ClientDeviceID: clientDeviceID, ClientDeviceName: clientDeviceName, ServerDeviceID: challenge.ServerDeviceID, ServerDeviceName: challenge.ServerDeviceName, ClientEphemeralPublicKey: clientEphemeral.Public, ServerEphemeralPublicKey: serverEphemeralPublicKey, ClientIdentityPublicKey: clientIdentity.Public, ServerIdentityPublicKey: serverIdentityPublicKey}
	ee, err := intcrypto.ComputeSharedSecret(clientEphemeral.Private, serverEphemeralPublicKey)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ee) error = %v", err)
	}
	ss, err := intcrypto.ComputeSharedSecret(clientIdentity.Private, serverIdentityPublicKey)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ss) error = %v", err)
	}
	confirmation, err := intcrypto.ComputePairingConfirmation([][]byte{ee, ss}, pin, transcript)
	if err != nil {
		t.Fatalf("ComputePairingConfirmation() error = %v", err)
	}
	return confirmation
}

func mustAppClientSession(t *testing.T, flow intcrypto.Flow, pairingID string, response iosPairingTestResponse, clientIdentity, clientEphemeral intcrypto.KeyPair, pin string) *session.StateMachine {
	t.Helper()
	sessionID := mustDecodeBase64(t, response.SessionID)
	serverIdentityPublicKey := mustDecodeBase64(t, response.ServerIdentityPublicKey)
	serverEphemeralPublicKey := mustDecodeBase64(t, response.ServerEphemeralPublicKey)
	transcript := intcrypto.Transcript{Version: protocol.VersionV1Alpha1, Flow: flow, PairingID: pairingID, ClientDeviceID: response.DeviceID, ClientDeviceName: response.DeviceName, ServerDeviceID: response.ServerDeviceID, ServerDeviceName: response.ServerDeviceName, ClientEphemeralPublicKey: clientEphemeral.Public, ServerEphemeralPublicKey: serverEphemeralPublicKey, ClientIdentityPublicKey: clientIdentity.Public, ServerIdentityPublicKey: serverIdentityPublicKey}
	ee, err := intcrypto.ComputeSharedSecret(clientEphemeral.Private, serverEphemeralPublicKey)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ee) error = %v", err)
	}
	ss, err := intcrypto.ComputeSharedSecret(clientIdentity.Private, serverIdentityPublicKey)
	if err != nil {
		t.Fatalf("ComputeSharedSecret(ss) error = %v", err)
	}
	var confirmation []byte
	if flow == intcrypto.FlowPairing {
		confirmation, err = intcrypto.ComputePairingConfirmation([][]byte{ee, ss}, pin, transcript)
	} else {
		confirmation, err = intcrypto.ComputeResumeConfirmation([][]byte{ee, ss}, transcript)
	}
	if err != nil {
		t.Fatalf("ComputeConfirmation() error = %v", err)
	}
	if !bytes.Equal(confirmation, mustDecodeBase64(t, response.ServerConfirmation)) {
		t.Fatal("server confirmation mismatch")
	}
	keys, err := intcrypto.DeriveSessionKeys([][]byte{ee, ss}, sessionID, transcript)
	if err != nil {
		t.Fatalf("DeriveSessionKeys() error = %v", err)
	}
	machine, err := session.NewStateMachine(session.Config{SessionID: sessionID, SendKey: keys.ClientToServerKey, ReceiveKey: keys.ServerToClientKey, InactivityTimeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("NewStateMachine() error = %v", err)
	}
	return machine
}

func pairHTTPClient(t *testing.T, baseURL string) (PairingStartResponse, iosPairingTestResponse, intcrypto.KeyPair) {
	t.Helper()
	startResp := mustPost(t, baseURL+"/v1/pairing/start", nil)
	var start PairingStartResponse
	decodeJSON(t, startResp, &start)
	startResp.Body.Close()
	challengeResp := mustGet(t, baseURL+"/v1/ios/pairing/challenge")
	var challenge iosPairingChallengeTestResponse
	decodeJSON(t, challengeResp, &challenge)
	challengeResp.Body.Close()
	clientIdentity, clientEphemeral := mustClientKeys(t)
	confirmation := mustAppPairingConfirmation(t, challenge, clientIdentity, clientEphemeral, "iphone-1", "ZVVZ", start.PIN)
	pairResp := mustPost(t, baseURL+"/v1/ios/pair", marshalJSONReader(t, iosPairingCompleteTestRequest{PairingID: challenge.PairingID, DeviceID: "iphone-1", DeviceName: "ZVVZ", ClientIdentityPublicKey: base64.StdEncoding.EncodeToString(clientIdentity.Public), ClientEphemeralPublicKey: base64.StdEncoding.EncodeToString(clientEphemeral.Public), ClientConfirmation: base64.StdEncoding.EncodeToString(confirmation)}))
	defer pairResp.Body.Close()
	var paired iosPairingTestResponse
	decodeJSON(t, pairResp, &paired)
	return start, paired, clientIdentity
}

func encryptTestAudioMessages(t *testing.T, machine *session.StateMachine, sessionID string) []session.EncryptedMessage {
	t.Helper()
	streamID := "stream-1"
	frames := [][]byte{bytes.Repeat([]byte{1}, asr.DefaultFrameSize), bytes.Repeat([]byte{2}, asr.DefaultFrameSize)}
	payloads := []struct {
		messageType protocol.MessageType
		payload     any
	}{
		{messageType: protocol.MessageTypeAudioStart, payload: protocol.AudioStart{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStart}, SessionID: sessionID, StreamID: streamID, Metadata: asr.DefaultAudioMetadata()}},
		{messageType: protocol.MessageTypeAudioFrame, payload: protocol.AudioFrame{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioFrame}, SessionID: sessionID, StreamID: streamID, Sequence: 1, PayloadBase64: base64.StdEncoding.EncodeToString(frames[0])}},
		{messageType: protocol.MessageTypeAudioFrame, payload: protocol.AudioFrame{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioFrame}, SessionID: sessionID, StreamID: streamID, Sequence: 2, PayloadBase64: base64.StdEncoding.EncodeToString(frames[1])}},
		{messageType: protocol.MessageTypeAudioStop, payload: protocol.AudioStop{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeAudioStop}, SessionID: sessionID, StreamID: streamID, LastSequence: len(frames)}},
	}
	messages := make([]session.EncryptedMessage, 0, len(payloads))
	for _, item := range payloads {
		encoded, err := protocol.Encode(item.payload)
		if err != nil {
			t.Fatalf("Encode(%T) error = %v", item.payload, err)
		}
		message, err := machine.Encrypt(item.messageType, encoded)
		if err != nil {
			t.Fatalf("Encrypt(%s) error = %v", item.messageType, err)
		}
		messages = append(messages, message)
	}
	return messages
}

func marshalJSONReader(t *testing.T, value any) *strings.Reader {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(%T) error = %v", value, err)
	}
	return strings.NewReader(string(data))
}

func mustDecodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("DecodeString(%q) error = %v", value, err)
	}
	return data
}

type iosPairingChallengeTestResponse struct {
	PairingID                string `json:"pairing_id"`
	ServerDeviceID           string `json:"server_device_id"`
	ServerDeviceName         string `json:"server_device_name"`
	ServerIdentityPublicKey  string `json:"server_identity_public_key"`
	ServerEphemeralPublicKey string `json:"server_ephemeral_public_key"`
}

type iosPairingCompleteTestRequest struct {
	PairingID                string `json:"pairing_id"`
	DeviceID                 string `json:"device_id"`
	DeviceName               string `json:"device_name"`
	ClientIdentityPublicKey  string `json:"client_identity_public_key"`
	ClientEphemeralPublicKey string `json:"client_ephemeral_public_key"`
	ClientConfirmation       string `json:"client_confirmation"`
}

type iosResumeTestRequest struct {
	PairingID                string `json:"pairing_id"`
	DeviceID                 string `json:"device_id"`
	DeviceName               string `json:"device_name"`
	ClientIdentityPublicKey  string `json:"client_identity_public_key"`
	ClientEphemeralPublicKey string `json:"client_ephemeral_public_key"`
}

type iosPairingTestResponse struct {
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

type blockingTestInjector struct {
	started chan string
	release chan struct{}

	mu    sync.Mutex
	count int
}

func newBlockingTestInjector() *blockingTestInjector {
	injector := &blockingTestInjector{
		started: make(chan string, 1),
		release: make(chan struct{}),
	}
	return injector
}

func (i *blockingTestInjector) Insert(ctx context.Context, text string) (inject.Receipt, error) {
	i.mu.Lock()
	i.count++
	i.mu.Unlock()

	select {
	case i.started <- text:
	default:
	}

	select {
	case <-i.release:
		return inject.Receipt{Target: "blocking", Status: "inserted"}, nil
	case <-ctx.Done():
		return inject.Receipt{}, ctx.Err()
	}
}

func (i *blockingTestInjector) calls() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.count
}
