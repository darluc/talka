package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
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
		return runServe(stdout, stderr, args[1:])
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
		_, _ = fmt.Fprintf(os.Stderr, "talka-asr-runtime: %v\n", err)
		os.Exit(1)
	}
}

func runServe(stdout, stderr io.Writer, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		_, err := fmt.Fprint(stdout, serveUsage())
		return err
	}

	addr := "127.0.0.1:19095"
	upstreamURL := envOrDefault("TALKA_FUNASR_UPSTREAM_URL", "ws://127.0.0.1:10095")
	mode := "2pass"
	crashMidstream := false
	ignoredFlagsWithValues := map[string]bool{
		"--model-dir":        true,
		"--online-model-dir": true,
		"--vad-dir":          true,
		"--punc-dir":         true,
		"--itn-dir":          true,
	}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--addr":
			index++
			if index >= len(args) {
				return errors.New("serve requires a value after --addr")
			}
			addr = args[index]
		case "--upstream-url":
			index++
			if index >= len(args) {
				return errors.New("serve requires a value after --upstream-url")
			}
			upstreamURL = args[index]
		case "--mode":
			index++
			if index >= len(args) {
				return errors.New("serve requires a value after --mode")
			}
			mode = args[index]
		case "--crash-midstream":
			crashMidstream = true
		default:
			if ignoredFlagsWithValues[args[index]] {
				index++
				if index >= len(args) {
					return fmt.Errorf("serve requires a value after %s", args[index-1])
				}
				continue
			}
			_, _ = fmt.Fprint(stderr, serveUsage())
			return fmt.Errorf("unknown serve argument %q", args[index])
		}
	}

	if err := validateLocalAddr(addr); err != nil {
		return err
	}

	var handler http.Handler
	if crashMidstream {
		handler = (&asr.CrashRuntime{Exit: os.Exit}).Handler()
	} else {
		runtime := asr.NewProxyRuntime(asr.ProxyRuntimeConfig{UpstreamURL: upstreamURL, Mode: mode})
		handler = runtime.Handler()
	}
	server := &http.Server{Addr: addr, Handler: handler}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	_, _ = fmt.Fprintf(stdout, "LISTEN ws://%s/ws\n", addr)

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

	url := "ws://127.0.0.1:19095/ws"
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
	_, err := fmt.Fprintf(stdout, "HEALTH ready runtime=funasr-proxy url=%s\n", url)
	return err
}

func runSmoke(stdout, stderr io.Writer, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		_, err := fmt.Fprint(stdout, smokeUsage())
		return err
	}

	url := "ws://127.0.0.1:19095/ws"
	fixture := ""
	crashMidstream := false
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
		case "--crash-midstream":
			crashMidstream = true
		default:
			_, _ = fmt.Fprint(stderr, smokeUsage())
			return fmt.Errorf("unknown smoke argument %q", args[index])
		}
	}

	if fixture == "" {
		_, _ = fmt.Fprint(stderr, smokeUsage())
		return errors.New("smoke requires --fixture <path>")
	}

	frames, err := asr.LoadWAVFrames(fixture, asr.DefaultFrameSize)
	if err != nil {
		return err
	}

	client := asr.NewClient(asr.Config{URL: url, Version: protocol.VersionV1Alpha1, Timeout: 30 * time.Second})
	result, err := client.Transcribe(context.Background(), asr.DefaultAudioMetadata(), frames)
	if err != nil {
		if crashMidstream && asr.IsRuntimeUnavailableError(err) {
			_, writeErr := fmt.Fprintf(stdout, "EXPECTED_ERROR %s %s\n", asr.ErrorCodeRuntimeUnavailable, strings.TrimSpace(err.Error()))
			return writeErr
		}
		return err
	}
	if crashMidstream {
		return errors.New("crash-midstream smoke unexpectedly completed without runtime failure")
	}
	if strings.TrimSpace(result.TextFinal.Text) == "" {
		return errors.New("real ASR smoke returned empty transcript")
	}
	_, err = fmt.Fprintln(stdout, strings.TrimSpace(result.TextFinal.Text))
	return err
}

func validateLocalAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("listen address %q must stay localhost-only", addr)
		}
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func mainUsage() string {
	return "usage: talka-asr-runtime <serve|health|smoke> [flags]\n"
}

func serveUsage() string {
	return "usage: talka-asr-runtime serve [--addr 127.0.0.1:19095] [--upstream-url ws://127.0.0.1:10095] [--mode 2pass] [--model-dir <path>] [--online-model-dir <path>] [--vad-dir <path>] [--punc-dir <path>] [--itn-dir <path>] [--crash-midstream]\n"
}

func healthUsage() string {
	return "usage: talka-asr-runtime health [--url ws://127.0.0.1:19095/ws]\n"
}

func smokeUsage() string {
	return "usage: talka-asr-runtime smoke --fixture <path> [--url ws://127.0.0.1:19095/ws] [--crash-midstream]\n"
}
