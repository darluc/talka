package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	funasrBinary := ""
	crashMidstream := false
	ignoredFlagsWithValues := map[string]bool{
		"--model-dir":       true,
		"--online-model-dir": true,
		"--vad-dir":         true,
		"--punc-dir":        true,
		"--itn-dir":         true,
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
		case "--funasr-binary":
			index++
			if index >= len(args) {
				return errors.New("serve requires a value after --funasr-binary")
			}
			funasrBinary = args[index]
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

	// When --funasr-binary is provided, start the C++ FunASR server as a child
	// subprocess on an ephemeral port, then proxy to it. This is the embedded
	// mode: the Go proxy manages the C++ server lifecycle internally.
	var funasrCmd *exec.Cmd
	var funasrCancel context.CancelFunc
	if funasrBinary != "" && !crashMidstream {
		resolved, err := resolveFunASRBinary(funasrBinary)
		if err != nil {
			return err
		}

		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("invalid addr %q: %w", addr, err)
		}

		funasrPort, err := reserveLoopbackPort(host)
		if err != nil {
			return fmt.Errorf("could not reserve ephemeral port for FunASR subprocess: %w", err)
		}

		funasrURL := fmt.Sprintf("ws://%s:%d/ws", host, funasrPort)
		upstreamURL = funasrURL // proxy to the C++ subprocess we're about to start

		funasrCtx, cancel := context.WithCancel(context.Background())
		funasrCancel = cancel

		funasrCmd, err = startFunASRSubprocess(funasrCtx, resolved, host, funasrPort, args, stdout, stderr)
		if err != nil {
			cancel()
			return fmt.Errorf("FunASR subprocess failed to start: %w", err)
		}

		_, _ = fmt.Fprintf(stdout, "FUNASR_SUBPROCESS pid=%d port=%d url=%s\n", funasrCmd.Process.Pid, funasrPort, funasrURL)

		if err := waitForFunASRHealth(funasrCtx, funasrURL, 180*time.Second); err != nil {
			cancel()
			_ = funasrCmd.Process.Kill()
			return fmt.Errorf("FunASR subprocess did not become healthy: %w", err)
		}

		_, _ = fmt.Fprintf(stdout, "FUNASR_HEALTHY url=%s\n", funasrURL)
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
		if funasrCancel != nil {
			funasrCancel()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		if funasrCmd != nil && funasrCmd.Process != nil {
			_ = funasrCmd.Process.Kill()
		}
		return shutdownErr
	case err := <-errCh:
		if funasrCancel != nil {
			funasrCancel()
		}
		if funasrCmd != nil && funasrCmd.Process != nil {
			_ = funasrCmd.Process.Kill()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// resolveFunASRBinary resolves the path to the C++ FunASR binary.
// If the path is absolute and the file exists, it's returned as-is.
// If the path is just a filename, it searches next to the running executable
// (useful for app bundles where both binaries live in the same directory).
func resolveFunASRBinary(path string) (string, error) {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("FunASR binary not found at %q: %w", path, err)
		}
		return path, nil
	}

	// Try next to the current executable (app bundle layout).
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), path)
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}

	// Try as-is (might be on PATH).
	if _, err := exec.LookPath(path); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("FunASR binary %q not found (tried absolute path, sibling of executable, and PATH)", path)
}

// startFunASRSubprocess starts the C++ FunASR server as a child process.
// It passes through model directory flags from the parent's args, and
// overrides the listen address to the given host:port.
func startFunASRSubprocess(ctx context.Context, binaryPath, host string, port int, parentArgs []string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	funasrArgs := buildFunASRArgs(parentArgs, host, port)

	cmd := exec.CommandContext(ctx, binaryPath, funasrArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("exec %q: %w", binaryPath, err)
	}

	return cmd, nil
}

// buildFunASRArgs extracts model directory flags from the parent's args
// and builds the C++ FunASR server command line.
func buildFunASRArgs(parentArgs []string, host string, port int) []string {
	args := []string{
		"--listen-ip", host,
		"--port", strconv.Itoa(port),
	}

	// Forward model directory flags from the Go proxy's args.
	flagMap := map[string]string{
		"--model-dir":       "--model-dir",
		"--online-model-dir": "--online-model-dir",
		"--vad-dir":         "--vad-dir",
		"--punc-dir":        "--punc-dir",
		"--itn-dir":         "--itn-dir",
	}
	for i := 0; i < len(parentArgs); i++ {
		if destFlag, ok := flagMap[parentArgs[i]]; ok && i+1 < len(parentArgs) {
			args = append(args, destFlag, parentArgs[i+1])
		}
	}

	return args
}

// waitForFunASRHealth polls the FunASR WebSocket endpoint until it responds
// or the timeout expires.
func waitForFunASRHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		client := asr.NewClient(asr.Config{URL: url, Version: protocol.VersionV1Alpha1, Timeout: 2 * time.Second})
		if err := client.HealthCheck(ctx); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s", timeout)
}

// reserveLoopbackPort reserves an ephemeral port on the given host.
func reserveLoopbackPort(host string) (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}
	return addr.Port, nil
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
	return "usage: talka-asr-runtime serve [--addr 127.0.0.1:19095] [--upstream-url ws://127.0.0.1:10095] [--funasr-binary <path>] [--mode 2pass] [--model-dir <path>] [--online-model-dir <path>] [--vad-dir <path>] [--punc-dir <path>] [--itn-dir <path>] [--crash-midstream]\n"
}

func healthUsage() string {
	return "usage: talka-asr-runtime health [--url ws://127.0.0.1:19095/ws]\n"
}

func smokeUsage() string {
	return "usage: talka-asr-runtime smoke --fixture <path> [--url ws://127.0.0.1:19095/ws] [--crash-midstream]\n"
}
