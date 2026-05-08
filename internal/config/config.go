package config

import "path/filepath"

const defaultConfigRelativePath = "Library/Application Support/Talka/config.yaml"

type Config struct {
	Server    ServerConfig    `json:"server" yaml:"server"`
	ASR       ASRConfig       `json:"asr" yaml:"asr"`
	LLM       LLMConfig       `json:"llm" yaml:"llm"`
	Injection InjectionConfig `json:"injection" yaml:"injection"`
	Logging   LoggingConfig   `json:"logging" yaml:"logging"`
}

type ServerConfig struct {
	BindHost    string `json:"bind_host" yaml:"bind_host"`
	Port        int    `json:"port" yaml:"port"`
	ServiceName string `json:"service_name" yaml:"service_name"`
}

type ASRConfig struct {
	Provider       string          `json:"provider" yaml:"provider"`
	RuntimePath    string          `json:"runtime_path" yaml:"runtime_path"`
	Host           string          `json:"host" yaml:"host"`
	Port           int             `json:"port" yaml:"port"`
	Mode           string          `json:"mode" yaml:"mode"`
	SampleRate     int             `json:"sample_rate" yaml:"sample_rate"`
	StartupTimeout int             `json:"startup_timeout_seconds" yaml:"startup_timeout_seconds"`
	ContainerImage string          `json:"container_image" yaml:"container_image"`
	ContainerName  string          `json:"container_name" yaml:"container_name"`
	DownloadDir    string          `json:"download_dir" yaml:"download_dir"`
	HotwordPath    string          `json:"hotword_path" yaml:"hotword_path"`
	Models         ASRModelsConfig `json:"models" yaml:"models"`
}

type ASRModelsConfig struct {
	ASR    string `json:"asr" yaml:"asr"`
	Online string `json:"online" yaml:"online"`
	VAD    string `json:"vad" yaml:"vad"`
	Punc   string `json:"punc" yaml:"punc"`
	ITN    string `json:"itn" yaml:"itn"`
	LM     string `json:"lm" yaml:"lm"`
}

type LLMConfig struct {
	Provider       string `json:"provider" yaml:"provider"`
	BaseURL        string `json:"base_url" yaml:"base_url"`
	Model          string `json:"model" yaml:"model"`
	TimeoutSeconds int    `json:"timeout_seconds" yaml:"timeout_seconds"`
}

type InjectionConfig struct {
	Mode             string `json:"mode" yaml:"mode"`
	RestoreClipboard bool   `json:"restore_clipboard" yaml:"restore_clipboard"`
}

type LoggingConfig struct {
	Level             string `json:"level" yaml:"level"`
	CaptureAudio      bool   `json:"capture_audio" yaml:"capture_audio"`
	CaptureTranscript bool   `json:"capture_transcript" yaml:"capture_transcript"`
}

func DefaultPath(home string) string {
	return filepath.Join(home, defaultConfigRelativePath)
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			BindHost:    "0.0.0.0",
			Port:        0,
			ServiceName: "Talka",
		},
		ASR: ASRConfig{
			Provider:       "funasr_embedded",
			RuntimePath:    "talka-asr-runtime",
			Host:           "127.0.0.1",
			Port:           10095,
			Mode:           "2pass",
			SampleRate:     16000,
			StartupTimeout: 180,
			ContainerImage: "registry.cn-hangzhou.aliyuncs.com/funasr_repo/funasr:funasr-runtime-sdk-online-cpu-0.1.13",
			ContainerName:  "talka-funasr",
			DownloadDir:    "funasr-downloads",
			HotwordPath:    "",
			Models: ASRModelsConfig{
				ASR:    "models/funasr/paraformer-zh-onnx",
				Online: "models/funasr/paraformer-zh-online-onnx",
				VAD:    "models/funasr/fsmn-vad-onnx",
				Punc:   "models/funasr/ct-punc-onnx",
				ITN:    "models/funasr/itn-zh",
				LM:     "",
			},
		},
		LLM: LLMConfig{
			Provider:       "ollama",
			BaseURL:        "http://localhost:11434",
			Model:          "qwen3:8b",
			TimeoutSeconds: 30,
		},
		Injection: InjectionConfig{
			Mode:             "clipboard_paste",
			RestoreClipboard: true,
		},
		Logging: LoggingConfig{
			Level:             "info",
			CaptureAudio:      false,
			CaptureTranscript: false,
		},
	}
}
