package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"talka/internal/asr"
	"talka/internal/config"
)

func TestNewASRProviderFromConfigBuildsEmbeddedFunASRProvider(t *testing.T) {
	root := t.TempDir()
	runtimePath := mustExecutablePath(t, root, "runtime/talka-asr-runtime")
	cfg := config.ASRConfig{
		Provider:       "funasr_embedded",
		RuntimePath:    runtimePath,
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		SampleRate:     16000,
		StartupTimeout: 180,
		Models: config.ASRModelsConfig{
			ASR:    mustPath(t, root, "models/asr"),
			Online: mustPath(t, root, "models/online"),
			VAD:    mustPath(t, root, "models/vad"),
			Punc:   mustPath(t, root, "models/punc"),
			ITN:    mustPath(t, root, "models/itn"),
		},
	}

	provider, err := newASRProviderFromConfig(cfg, root)
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}

	managed, ok := provider.(*asr.ManagedSidecarProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *asr.ManagedSidecarProvider", provider)
	}
	if !managed.ManagerAlwaysEphemeral() {
		t.Fatal("ManagerAlwaysEphemeral() = false, want true for funasr_embedded")
	}
	if _, ok := provider.(asr.StreamingProvider); !ok {
		t.Fatalf("provider type = %T, want streaming FunASR provider", provider)
	}
}

func TestNewASRProviderFromConfigBuildsFunASRAliasAsStreamingEmbeddedProvider(t *testing.T) {
	root := t.TempDir()
	runtimePath := mustExecutablePath(t, root, "runtime/talka-asr-runtime")
	cfg := config.ASRConfig{
		Provider:       "funasr",
		RuntimePath:    runtimePath,
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		SampleRate:     16000,
		StartupTimeout: 180,
		Models: config.ASRModelsConfig{
			ASR:    mustPath(t, root, "models/asr"),
			Online: mustPath(t, root, "models/online"),
			VAD:    mustPath(t, root, "models/vad"),
			Punc:   mustPath(t, root, "models/punc"),
			ITN:    mustPath(t, root, "models/itn"),
		},
	}

	provider, err := newASRProviderFromConfig(cfg, root)
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}

	managed, ok := provider.(*asr.ManagedSidecarProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *asr.ManagedSidecarProvider", provider)
	}
	if !managed.ManagerAlwaysEphemeral() {
		t.Fatal("ManagerAlwaysEphemeral() = false, want true for funasr")
	}
	if _, ok := provider.(asr.StreamingProvider); !ok {
		t.Fatalf("provider type = %T, want streaming FunASR provider", provider)
	}
}

func TestNewASRProviderFromConfigBuildsExternalFunASRProvider(t *testing.T) {
	cfg := config.ASRConfig{
		Provider:       "funasr_external",
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		SampleRate:     16000,
		StartupTimeout: 180,
	}

	provider, err := newASRProviderFromConfig(cfg, t.TempDir())
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}

	if _, ok := provider.(*asr.UpstreamProvider); !ok {
		t.Fatalf("provider type = %T, want *asr.UpstreamProvider", provider)
	}
}

func TestSherpaONNXModelPathForPrecisionSwitchesParaformerFiles(t *testing.T) {
	root := t.TempDir()
	int8Encoder := filepath.Join(root, "encoder.int8.onnx")
	int8Decoder := filepath.Join(root, "decoder.int8.onnx")
	fp32Encoder := filepath.Join(root, "encoder.onnx")
	fp32Decoder := filepath.Join(root, "decoder.onnx")

	if got := sherpaONNXModelPathForPrecision("paraformer", "fp32", int8Encoder); got != fp32Encoder {
		t.Fatalf("fp32 encoder = %q, want %q", got, fp32Encoder)
	}
	if got := sherpaONNXModelPathForPrecision("paraformer", "fp32", int8Decoder); got != fp32Decoder {
		t.Fatalf("fp32 decoder = %q, want %q", got, fp32Decoder)
	}
	if got := sherpaONNXModelPathForPrecision("paraformer", "int8", fp32Encoder); got != int8Encoder {
		t.Fatalf("int8 encoder = %q, want %q", got, int8Encoder)
	}
	if got := sherpaONNXModelPathForPrecision("paraformer", "int8", fp32Decoder); got != int8Decoder {
		t.Fatalf("int8 decoder = %q, want %q", got, int8Decoder)
	}
	if got := sherpaONNXModelPathForPrecision("transducer", "fp32", int8Encoder); got != int8Encoder {
		t.Fatalf("transducer path = %q, want unchanged %q", got, int8Encoder)
	}
}

func TestASRRuntimeArgsFromConfigUsesProxyRuntimeServeInterface(t *testing.T) {
	args := asrRuntimeArgsFromConfig(config.ASRConfig{
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		StartupTimeout: 180,
		Models: config.ASRModelsConfig{
			ASR:    "/tmp/asr",
			Online: "/tmp/online",
			VAD:    "/tmp/vad",
			Punc:   "/tmp/punc",
			ITN:    "/tmp/itn",
			LM:     "",
		},
		HotwordPath: "",
	})

	assertContainsArg(t, args, "serve")
	assertContainsArgPair(t, args, "--addr", "127.0.0.1:19095")
	assertContainsArgPair(t, args, "--upstream-url", "ws://127.0.0.1:10095/ws")
	assertContainsArgPair(t, args, "--mode", "2pass")
	assertContainsArgPair(t, args, "--model-dir", "/tmp/asr")
	assertDoesNotContainArg(t, args, "--listen-ip")
	assertDoesNotContainArg(t, args, "--port")
	assertDoesNotContainArg(t, args, "--lm-dir")
	assertDoesNotContainArg(t, args, "--hotword")
	assertDoesNotContainArg(t, args, "--funasr-binary") // not set when FunASRBinaryPath is empty
}

func TestASRRuntimeArgsFromConfigIncludesFunASRBinaryPath(t *testing.T) {
	args := asrRuntimeArgsFromConfig(config.ASRConfig{
		Host:             "127.0.0.1",
		Port:             10095,
		Mode:             "2pass",
		StartupTimeout:   180,
		FunASRBinaryPath: "/usr/local/bin/funasr-wss-server-2pass",
		Models: config.ASRModelsConfig{
			ASR:    "/tmp/asr",
			Online: "/tmp/online",
			VAD:    "/tmp/vad",
			Punc:   "/tmp/punc",
			ITN:    "/tmp/itn",
		},
	})

	assertContainsArgPair(t, args, "--funasr-binary", "/usr/local/bin/funasr-wss-server-2pass")
}

func TestASRRuntimeArgsFromConfigIncludesModelPathsForRuntimeCompatibility(t *testing.T) {
	args := asrRuntimeArgsFromConfig(config.ASRConfig{
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		StartupTimeout: 180,
		Models: config.ASRModelsConfig{
			ASR:    "/tmp/asr",
			Online: "/tmp/online",
			VAD:    "/tmp/vad",
			Punc:   "/tmp/punc",
			ITN:    "/tmp/itn",
			LM:     "/tmp/lm",
		},
		HotwordPath: "/tmp/hotwords.txt",
	})

	assertContainsArgPair(t, args, "--model-dir", "/tmp/asr")
	assertContainsArgPair(t, args, "--online-model-dir", "/tmp/online")
	assertContainsArgPair(t, args, "--vad-dir", "/tmp/vad")
	assertContainsArgPair(t, args, "--punc-dir", "/tmp/punc")
	assertContainsArgPair(t, args, "--itn-dir", "/tmp/itn")
}

func TestASRRuntimeArgsFromConfigIncludesConfiguredTimeoutInManagerConfigPath(t *testing.T) {
	root := t.TempDir()
	runtimePath := mustExecutablePath(t, root, "runtime/talka-asr-runtime")
	cfg := config.ASRConfig{
		Provider:       "funasr_embedded",
		RuntimePath:    runtimePath,
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		SampleRate:     16000,
		StartupTimeout: 42,
		Models: config.ASRModelsConfig{
			ASR:    mustPath(t, root, "models/asr"),
			Online: mustPath(t, root, "models/online"),
			VAD:    mustPath(t, root, "models/vad"),
			Punc:   mustPath(t, root, "models/punc"),
			ITN:    mustPath(t, root, "models/itn"),
		},
	}

	provider, err := newASRProviderFromConfig(cfg, root)
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}

	managed, ok := provider.(*asr.ManagedSidecarProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *asr.ManagedSidecarProvider", provider)
	}

	if got, want := managed.ManagerStartupTimeout(), 42; got != want {
		t.Fatalf("ManagerStartupTimeout() = %d, want %d", got, want)
	}
}

func TestEmbeddedFunASRProviderSpeaksTalkaSidecarProtocol(t *testing.T) {
	root := t.TempDir()
	cfg := config.ASRConfig{
		Provider:       "funasr_embedded",
		RuntimePath:    mustSidecarHelperScript(t, root),
		Host:           "127.0.0.1",
		Port:           10095,
		Mode:           "2pass",
		SampleRate:     16000,
		StartupTimeout: 2,
		Models: config.ASRModelsConfig{
			ASR:    mustPath(t, root, "models/asr"),
			Online: mustPath(t, root, "models/online"),
			VAD:    mustPath(t, root, "models/vad"),
			Punc:   mustPath(t, root, "models/punc"),
			ITN:    mustPath(t, root, "models/itn"),
		},
	}

	provider, err := newASRProviderFromConfig(cfg, root)
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}
	t.Cleanup(func() {
		if shutdowner, ok := provider.(interface{ Shutdown(context.Context) error }); ok {
			_ = shutdowner.Shutdown(context.Background())
		}
	})

	result, err := provider.Transcribe(context.Background(), asr.Request{
		Metadata: asr.DefaultAudioMetadata(),
		Frames:   [][]byte{make([]byte, asr.DefaultFrameSize), make([]byte, asr.DefaultFrameSize)},
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := result.Transcript, "你好，世界"; got != want {
		t.Fatalf("Transcript = %q, want %q", got, want)
	}
}

func TestEmbeddedSidecarRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("TALKA_EMBEDDED_SIDECAR_HELPER") != "1" {
		return
	}

	addr := "127.0.0.1:19095"
	args := os.Args
	for index, arg := range args {
		if arg == "--" {
			args = args[index+1:]
			break
		}
	}
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--addr" {
			addr = args[index+1]
		}
	}

	server := &http.Server{Addr: addr, Handler: (&asr.FakeRuntime{Ready: true}).Handler()}
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func mustSidecarHelperScript(t *testing.T, root string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	path := filepath.Join(root, "runtime", "sidecar-helper.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	script := fmt.Sprintf("#!/bin/sh\nTALKA_EMBEDDED_SIDECAR_HELPER=1 exec %s -test.run=TestEmbeddedSidecarRuntimeHelperProcess -- \"$@\"\n", shellQuote(exe))
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func mustPath(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	return path
}

func assertContainsArgPair(t *testing.T, args []string, key, want string) {
	t.Helper()
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key {
			if args[index+1] != want {
				t.Fatalf("%s value = %q, want %q", key, args[index+1], want)
			}
			return
		}
	}
	t.Fatalf("args missing %s: %v", key, args)
}

func assertContainsArg(t *testing.T, args []string, want string) {
	t.Helper()
	for _, arg := range args {
		if arg == want {
			return
		}
	}
	t.Fatalf("args missing %s: %v", want, args)
}

func assertDoesNotContainArg(t *testing.T, args []string, unwanted string) {
	t.Helper()
	for _, arg := range args {
		if arg == unwanted {
			t.Fatalf("args contains %s: %v", unwanted, args)
		}
	}
}

func mustExecutablePath(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
