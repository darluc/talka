package asr

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"talka/internal/protocol"
)

func TestRuntimeManagerValidateRejectsRemoteHost(t *testing.T) {
	config := RuntimeManagerConfig{
		RuntimePath: mustExecutable(t),
		Host:        "0.0.0.0",
		Port:        19095,
		Models:      mustModelPaths(t),
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want localhost-only validation failure")
	}
	if !strings.Contains(err.Error(), "localhost") {
		t.Fatalf("Validate() error = %q, want localhost-only guidance", err)
	}
}

func TestRuntimeManagerValidateRejectsMissingPaths(t *testing.T) {
	models := mustModelPaths(t)
	models.ITN = filepath.Join(t.TempDir(), "missing-itn")

	config := RuntimeManagerConfig{
		RuntimePath: filepath.Join(t.TempDir(), "missing-runtime"),
		Host:        "127.0.0.1",
		Port:        19095,
		Models:      models,
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want missing path failure")
	}
	mustContainErrorParts(t, err.Error(), []string{"runtime_path", "models.itn", "must exist"})
}

func TestManagedProviderReturnsTypedRuntimeUnavailableAndRestarts(t *testing.T) {
	exe := mustExecutable(t)
	models := mustModelPaths(t)
	port := mustFreePort(t)
	marker := filepath.Join(t.TempDir(), "crashed-once")

	manager := NewRuntimeManager(RuntimeManagerConfig{
		RuntimePath:    exe,
		RuntimeArgs:    []string{"-test.run=TestRuntimeManagerHelperProcess", "--", "--addr", fmt.Sprintf("127.0.0.1:%d", port), "--marker", marker},
		Host:           "127.0.0.1",
		Port:           port,
		Models:         models,
		StartupTimeout: 2 * time.Second,
		StopTimeout:    time.Second,
		Env:            []string{"TALKA_RUNTIME_HELPER_PROCESS=1"},
	})
	t.Cleanup(func() {
		_ = manager.Stop(context.Background())
	})

	provider := NewManagedProvider(manager, Config{Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	request := Request{Metadata: DefaultAudioMetadata(), Frames: [][]byte{make([]byte, 640), make([]byte, 640)}}

	_, err := provider.Transcribe(context.Background(), request)
	if err == nil {
		t.Fatal("Transcribe() error = nil, want runtime unavailable error")
	}
	if got, want := RuntimeErrorCodeOf(err), ErrorCodeRuntimeUnavailable; got != want {
		t.Fatalf("RuntimeErrorCodeOf() = %q, want %q", got, want)
	}

	result, err := provider.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatalf("Transcribe() second call error = %v", err)
	}
	if got, want := result.Transcript, "你好，世界"; got != want {
		t.Fatalf("Transcript = %q, want %q", got, want)
	}
}

func TestUpstreamRuntimeManagerFallsBackToFreePortWhenPreferredPortIsBusy(t *testing.T) {
	exe := mustExecutable(t)
	models := mustModelPaths(t)
	preferredPort := mustFreePort(t)
	occupied, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err != nil {
		t.Fatalf("Listen(preferred port) error = %v", err)
	}
	defer occupied.Close()

	manager := NewUpstreamRuntimeManager(UpstreamRuntimeManagerConfig{
		RuntimePath: exe,
		RuntimeArgs: []string{
			"-test.run=TestRuntimeManagerHelperProcess",
			"--",
			"--listen-ip", "127.0.0.1",
			"--port", fmt.Sprintf("%d", preferredPort),
		},
		Host:           "127.0.0.1",
		Port:           preferredPort,
		Models:         models,
		StartupTimeout: 2 * time.Second,
		StopTimeout:    time.Second,
		Env:            []string{"TALKA_RUNTIME_HELPER_PROCESS=1"},
	})
	t.Cleanup(func() {
		_ = manager.Stop(context.Background())
	})

	if err := manager.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning() error = %v", err)
	}

	if got := manager.URL(); got == fmt.Sprintf("ws://127.0.0.1:%d", preferredPort) {
		t.Fatalf("URL() = %q, want fallback port different from busy preferred port", got)
	}
}

func TestRuntimeManagerHelperProcess(t *testing.T) {
	if os.Getenv("TALKA_RUNTIME_HELPER_PROCESS") != "1" {
		return
	}

	fs := flag.NewFlagSet("runtime-helper", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:19095", "listen address")
	listenIP := fs.String("listen-ip", "", "listen ip")
	port := fs.Int("port", 0, "listen port")
	marker := fs.String("marker", "", "crash marker")
	_ = fs.Parse(os.Args[3:])
	if *listenIP != "" && *port > 0 {
		*addr = net.JoinHostPort(*listenIP, fmt.Sprintf("%d", *port))
	}

	var handler http.Handler
	if *listenIP != "" && *port > 0 {
		handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			conn, err := acceptWebSocket(w, req)
			if err != nil {
				return
			}
			_ = conn.Close()
		})
	} else if *marker != "" && !pathExists(*marker) {
		if err := os.WriteFile(*marker, []byte("crashed"), 0o644); err != nil {
			panic(err)
		}
		handler = crashAfterFirstFrameHandler{}
	} else {
		handler = (&FakeRuntime{Ready: true}).Handler()
	}

	server := &http.Server{Addr: *addr, Handler: handler}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
	os.Exit(0)
}

type crashAfterFirstFrameHandler struct{}

func (crashAfterFirstFrameHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	conn, err := acceptWebSocket(w, req)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		msg, err := readMessage(context.Background(), conn)
		if err != nil {
			return
		}

		switch typed := msg.(type) {
		case protocol.ClientHello:
			_ = conn.WriteJSON(protocol.ServerHello{Envelope: protocol.Envelope{Version: protocol.VersionV1Alpha1, Type: protocol.MessageTypeServerHello}, RuntimeName: "helper-crash", Ready: true})
		case protocol.AudioStart:
			if err := protocol.ValidateAudioMetadata(typed.Metadata); err != nil {
				writeProtocolError(conn, protocol.ErrorCodeOf(err), err.Error())
				return
			}
		case protocol.AudioFrame:
			os.Exit(86)
		default:
			writeProtocolError(conn, protocol.ErrorCodeUnexpectedMessageType, "unexpected helper message")
			return
		}
	}
}

func mustExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	return exe
}

func mustModelPaths(t *testing.T) ModelPaths {
	t.Helper()
	root := t.TempDir()
	paths := ModelPaths{
		ASR:    filepath.Join(root, "asr"),
		Online: filepath.Join(root, "online"),
		VAD:    filepath.Join(root, "vad"),
		Punc:   filepath.Join(root, "punc"),
		ITN:    filepath.Join(root, "itn"),
	}
	for _, path := range []string{paths.ASR, paths.Online, paths.VAD, paths.Punc, paths.ITN} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
	}
	return paths
}

func mustContainErrorParts(t *testing.T, got string, parts []string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(got, part) {
			t.Fatalf("%q does not contain %q", got, part)
		}
	}
}

func mustFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
