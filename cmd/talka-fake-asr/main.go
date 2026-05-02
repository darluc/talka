package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"talka/internal/asr"
	"talka/internal/protocol"
)

func run(stdout, stderr io.Writer, args []string) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, mainUsage())
		return errors.New("missing subcommand")
	}

	switch args[0] {
	case "serve":
		return runServe(stdout, args[1:])
	case "health":
		return runHealth(stdout, stderr, args[1:])
	case "smoke":
		return runSmoke(stdout, stderr, args[1:])
	case "--help", "-h", "help":
		_, err := fmt.Fprint(stdout, mainUsage())
		return err
	default:
		_, _ = fmt.Fprint(stderr, mainUsage())
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func main() {
	if err := run(os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "talka-fake-asr: %v\n", err)
		os.Exit(1)
	}
}

func runServe(stdout io.Writer, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		_, err := fmt.Fprint(stdout, serveUsage())
		return err
	}

	addr := "127.0.0.1:19080"
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--addr":
			index++
			if index >= len(args) {
				return errors.New("serve requires a value after --addr")
			}
			addr = args[index]
		default:
			return fmt.Errorf("unknown serve argument %q", args[index])
		}
	}

	runtime := &asr.FakeRuntime{Ready: true}
	server := &http.Server{Addr: addr, Handler: runtime.Handler()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	_, _ = fmt.Fprintf(stdout, "LISTEN %s\n", websocketURL(addr))

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runHealth(stdout, stderr io.Writer, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		_, err := fmt.Fprint(stdout, healthUsage())
		return err
	}

	url := asr.DefaultURL
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--url":
			index++
			if index >= len(args) {
				return errors.New("health requires a value after --url")
			}
			url = args[index]
		default:
			_, _ = fmt.Fprint(stderr, healthUsage())
			return fmt.Errorf("unknown health argument %q", args[index])
		}
	}

	client := asr.NewClient(asr.Config{URL: url, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	if err := client.HealthCheck(context.Background()); err != nil {
		return err
	}

	_, err := fmt.Fprintf(stdout, "HEALTH ready runtime=fake-asr url=%s\n", url)
	return err
}

func runSmoke(stdout, stderr io.Writer, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		_, err := fmt.Fprint(stdout, smokeUsage())
		return err
	}

	url := asr.DefaultURL
	fixture := ""
	sampleRate := 16000
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--url":
			index++
			if index >= len(args) {
				return errors.New("smoke requires a value after --url")
			}
			url = args[index]
		case "--fixture":
			index++
			if index >= len(args) {
				return errors.New("smoke requires a value after --fixture")
			}
			fixture = args[index]
		case "--sample-rate":
			index++
			if index >= len(args) {
				return errors.New("smoke requires a value after --sample-rate")
			}
			parsed, err := strconv.Atoi(args[index])
			if err != nil {
				return fmt.Errorf("sample rate must be an integer: %w", err)
			}
			sampleRate = parsed
		default:
			_, _ = fmt.Fprint(stderr, smokeUsage())
			return fmt.Errorf("unknown smoke argument %q", args[index])
		}
	}

	if fixture == "" {
		_, _ = fmt.Fprint(stderr, smokeUsage())
		return errors.New("smoke requires --fixture <path>")
	}

	bytes, err := os.ReadFile(fixture)
	if err != nil {
		return err
	}

	frames := splitFrames(bytes, 640)
	client := asr.NewClient(asr.Config{URL: url, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
	if err := client.HealthCheck(context.Background()); err != nil {
		return err
	}

	metadata := protocol.AudioMetadata{SampleRate: sampleRate, Channels: 1, Encoding: protocol.EncodingPCMS16LE, FrameDurationMS: 20, Language: "zh-CN"}
	result, err := client.Transcribe(context.Background(), metadata, frames)
	if err != nil {
		if sampleRate != 16000 {
			_, printErr := fmt.Fprintf(stdout, "ERROR %s %s\n", protocol.ErrorCodeOf(err), strings.TrimSpace(err.Error()))
			return printErr
		}
		return err
	}

	if err := validateDeterministicOutputs(result); err != nil {
		return err
	}

	for _, partial := range result.Partials {
		_, _ = fmt.Fprintf(stdout, "PARTIAL %d %s\n", partial.SegmentIndex, partial.Text)
	}
	for _, final := range result.Finals {
		_, _ = fmt.Fprintf(stdout, "ASR_FINAL %d %s\n", final.SegmentIndex, final.Text)
	}
	_, _ = fmt.Fprintf(stdout, "TEXT_FINAL %s\n", result.TextFinal.Text)

	invalidMetadata := metadata
	invalidMetadata.Encoding = "opus"
	_, err = client.Transcribe(context.Background(), invalidMetadata, frames)
	if err == nil {
		return errors.New("invalid metadata check unexpectedly succeeded")
	}

	code := protocol.ErrorCodeOf(err)
	if code != protocol.ErrorCodeUnsupportedEncoding {
		return fmt.Errorf("invalid metadata error code = %q, want %q", code, protocol.ErrorCodeUnsupportedEncoding)
	}

	_, err = fmt.Fprintf(stdout, "ERROR %s %s\n", code, strings.TrimSpace(err.Error()))
	return err
}

func splitFrames(data []byte, frameSize int) [][]byte {
	frames := make([][]byte, 0, (len(data)+frameSize-1)/frameSize)
	for len(data) > 0 {
		next := frameSize
		if len(data) < frameSize {
			next = len(data)
		}
		frame := make([]byte, next)
		copy(frame, data[:next])
		frames = append(frames, frame)
		data = data[next:]
	}
	return frames
}

func validateDeterministicOutputs(result asr.StreamResult) error {
	if len(result.Partials) != 2 || result.Partials[0].Text != "你好" || result.Partials[1].Text != "你好，世界" {
		return errors.New("fake runtime partial outputs changed")
	}
	if len(result.Finals) != 1 || result.Finals[0].Text != "你好，世界" {
		return errors.New("fake runtime ASR final output changed")
	}
	if result.TextFinal.Text != "你好，世界" {
		return errors.New("fake runtime text final output changed")
	}
	return nil
}

func websocketURL(addr string) string {
	return "ws://" + addr + "/ws"
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func mainUsage() string {
	return "usage: talka-fake-asr <serve|health|smoke> [flags]\n"
}

func serveUsage() string {
	return "usage: talka-fake-asr serve [--addr 127.0.0.1:19080]\n"
}

func healthUsage() string {
	return "usage: talka-fake-asr health [--url ws://127.0.0.1:19080/ws]\n"
}

func smokeUsage() string {
	return "usage: talka-fake-asr smoke --fixture <path> [--url ws://127.0.0.1:19080/ws] [--sample-rate 16000]\n"
}
