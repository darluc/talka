package app

import (
	"os"
	"path/filepath"
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
			ASR:     mustPath(t, root, "models/asr"),
			Online:  mustPath(t, root, "models/online"),
			VAD:     mustPath(t, root, "models/vad"),
			Punc:    mustPath(t, root, "models/punc"),
			ITN:     mustPath(t, root, "models/itn"),
		},
	}

	provider, err := newASRProviderFromConfig(cfg, root)
	if err != nil {
		t.Fatalf("newASRProviderFromConfig() error = %v", err)
	}

	upstream, ok := provider.(*asr.UpstreamProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *asr.UpstreamProvider", provider)
	}
	if !upstream.ManagerAlwaysEphemeral() {
		t.Fatal("ManagerAlwaysEphemeral() = false, want true for funasr_embedded")
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

func TestASRRuntimeArgsFromConfigUsesProxyRuntimeServeInterface(t *testing.T) {
	args := asrRuntimeArgsFromConfig(config.ASRConfig{
		Host: "127.0.0.1",
		Port: 10095,
		Mode: "2pass",
		StartupTimeout: 180,
		Models: config.ASRModelsConfig{
			ASR:     "/tmp/asr",
			Online:  "/tmp/online",
			VAD:     "/tmp/vad",
			Punc:    "/tmp/punc",
			ITN:     "/tmp/itn",
			LM:      "",
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
		Host:            "127.0.0.1",
		Port:            10095,
		Mode:            "2pass",
		StartupTimeout:  180,
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

	upstream, ok := provider.(*asr.UpstreamProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *asr.UpstreamProvider", provider)
	}

	if got, want := upstream.ManagerStartupTimeout(), 42; got != want {
		t.Fatalf("ManagerStartupTimeout() = %d, want %d", got, want)
	}
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
