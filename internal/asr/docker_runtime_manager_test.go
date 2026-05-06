package asr

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRunArgsIncludeOnlineModelAndManagedMount(t *testing.T) {
	downloadDir := filepath.Join(t.TempDir(), "downloads")
	args, err := dockerRunArgs(DockerRuntimeManagerConfig{
		Host:          "127.0.0.1",
		Port:          10095,
		Image:         "funasr:test",
		ContainerName: "talka-funasr",
		DownloadDir:   downloadDir,
		Models: ModelPaths{
			ASR:    "offline-model",
			Online: "online-model",
			VAD:    "vad-model",
			Punc:   "punc-model",
			ITN:    "itn-model",
		},
	}, 13095)
	if err != nil {
		t.Fatalf("dockerRunArgs() error = %v", err)
	}

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--name talka-funasr",
		"-p 127.0.0.1:13095:10095",
		"-v " + downloadDir + ":/workspace/models",
		"funasr:test",
		"--model-dir offline-model",
		"--online-model-dir online-model",
		"--vad-dir vad-model",
		"--punc-dir punc-model",
		"--itn-dir itn-model",
		"--certfile 0",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("dockerRunArgs() missing %q in %q", want, joined)
		}
	}
}

func TestDockerRunArgsPassesOptionalLMAndHotwordSettings(t *testing.T) {
	downloadDir := filepath.Join(t.TempDir(), "downloads")
	args, err := dockerRunArgs(DockerRuntimeManagerConfig{
		Host:          "127.0.0.1",
		Port:          10095,
		Image:         "funasr:test",
		ContainerName: "talka-funasr",
		DownloadDir:   downloadDir,
		HotwordPath:   "/workspace/hotwords.txt",
		Models: ModelPaths{
			ASR:    "offline-model",
			Online: "online-model",
			VAD:    "vad-model",
			Punc:   "punc-model",
			ITN:    "itn-model",
			LM:     "lm-model",
		},
	}, 10095)
	if err != nil {
		t.Fatalf("dockerRunArgs() error = %v", err)
	}

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--lm-dir lm-model",
		"--hotword /workspace/hotwords.txt",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("dockerRunArgs() missing %q in %q", want, joined)
		}
	}
}

func TestParseDockerPublishedPort(t *testing.T) {
	port, err := parseDockerPublishedPort("0.0.0.0:10095\n[::]:10095\n")
	if err != nil {
		t.Fatalf("parseDockerPublishedPort() error = %v", err)
	}
	if got, want := port, 10095; got != want {
		t.Fatalf("parseDockerPublishedPort() = %d, want %d", got, want)
	}
}
