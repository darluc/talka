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
	Provider       string           `json:"provider" yaml:"provider"`
	Host           string           `json:"host" yaml:"host"`
	Port           int              `json:"port" yaml:"port"`
	Mode           string           `json:"mode" yaml:"mode"`
	SampleRate     int              `json:"sample_rate" yaml:"sample_rate"`
	StartupTimeout int              `json:"startup_timeout_seconds" yaml:"startup_timeout_seconds"`
	SherpaONNX     SherpaONNXConfig `json:"sherpa_onnx" yaml:"sherpa_onnx"`
}

type SherpaONNXConfig struct {
	ModelProfile   string `json:"model_profile" yaml:"model_profile"`
	ModelType      string `json:"model_type" yaml:"model_type"`
	Precision      string `json:"precision" yaml:"precision"`
	TokensPath     string `json:"tokens_path" yaml:"tokens_path"`
	EncoderPath    string `json:"encoder_path" yaml:"encoder_path"`
	DecoderPath    string `json:"decoder_path" yaml:"decoder_path"`
	JoinerPath     string `json:"joiner_path" yaml:"joiner_path"`
	NumThreads     int    `json:"num_threads" yaml:"num_threads"`
	DecodingMethod string `json:"decoding_method" yaml:"decoding_method"`
	FeatureDim     int    `json:"feature_dim" yaml:"feature_dim"`
	Provider       string `json:"provider" yaml:"provider"`
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
			Provider:       "onnx",
			Host:           "127.0.0.1",
			Port:           10095,
			Mode:           "streaming",
			SampleRate:     16000,
			StartupTimeout: 180,
			SherpaONNX: SherpaONNXConfig{
				ModelProfile:   "paraformer-bilingual",
				ModelType:      "paraformer",
				Precision:      "int8",
				TokensPath:     "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en/tokens.txt",
				EncoderPath:    "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en/encoder.int8.onnx",
				DecoderPath:    "models/sherpa-onnx/streaming-paraformer-bilingual-zh-en/decoder.int8.onnx",
				JoinerPath:     "",
				NumThreads:     2,
				DecodingMethod: "greedy_search",
				FeatureDim:     80,
				Provider:       "cpu",
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
