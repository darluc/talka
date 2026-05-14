package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"talka/internal/asr"
)

type modelFiles struct {
	tokens  string
	encoder string
	decoder string
	joiner  string
}

func modelFilesForPrecision(modelDir, precision string) modelFiles {
	if strings.EqualFold(strings.TrimSpace(precision), "fp32") {
		return modelFiles{
			tokens:  filepath.Join(modelDir, "tokens.txt"),
			encoder: filepath.Join(modelDir, "encoder.onnx"),
			decoder: filepath.Join(modelDir, "decoder.onnx"),
		}
	}
	return modelFiles{
		tokens:  filepath.Join(modelDir, "tokens.txt"),
		encoder: filepath.Join(modelDir, "encoder.int8.onnx"),
		decoder: filepath.Join(modelDir, "decoder.int8.onnx"),
	}
}

func main() {
	if err := run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "talka-sherpa-transcribe: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("talka-sherpa-transcribe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fixture := fs.String("fixture", "", "16kHz mono PCM WAV fixture")
	modelDir := fs.String("model-dir", "models/sherpa-onnx/streaming-paraformer-trilingual-zh-cantonese-en", "sherpa-onnx model directory")
	precision := fs.String("precision", "int8", "model precision: int8 or fp32")
	numThreads := fs.Int("threads", 2, "sherpa-onnx inference threads")
	provider := fs.String("provider", "cpu", "sherpa-onnx execution provider")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*fixture) == "" {
		return fmt.Errorf("--fixture is required")
	}
	if *precision != "int8" && *precision != "fp32" {
		return fmt.Errorf("--precision must be int8 or fp32")
	}

	files := modelFilesForPrecision(*modelDir, *precision)
	for _, path := range []string{*fixture, files.tokens, files.encoder, files.decoder} {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}

	frames, err := asr.LoadWAVFrames(*fixture, asr.DefaultFrameSize)
	if err != nil {
		return err
	}

	started := time.Now()
	asrProvider, err := asr.NewSherpaONNXProvider(asr.SherpaONNXConfig{
		ModelType:      "paraformer",
		Precision:      *precision,
		TokensPath:     files.tokens,
		EncoderPath:    files.encoder,
		DecoderPath:    files.decoder,
		NumThreads:     *numThreads,
		DecodingMethod: "greedy_search",
		FeatureDim:     80,
		Provider:       *provider,
	})
	if err != nil {
		return err
	}
	defer func() { _ = asrProvider.Shutdown(context.Background()) }()

	result, err := asrProvider.Transcribe(ctx, asr.Request{
		Metadata: asr.DefaultAudioMetadata(),
		Frames:   frames,
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "TRANSCRIPT\t%s\t%d\t%s\n", *precision, time.Since(started).Milliseconds(), result.Transcript)
	return nil
}
