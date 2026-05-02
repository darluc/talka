package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"talka/internal/inject"
)

func main() {
	if err := run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "qapaste: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, stdout, stderr *os.File, args []string) error {
	fs := flag.NewFlagSet("qapaste", flag.ContinueOnError)
	fs.SetOutput(stderr)

	mode := fs.String("mode", "real", "real|simulate-success|simulate-user-changed|simulate-permission-denied")
	text := fs.String("text", "", "explicit QA text to paste")
	appLabel := fs.String("app-label", "unknown", "app or scenario label for output")
	expectErrorCode := fs.String("expect-error-code", "", "expected typed error code")
	restoreClipboard := fs.Bool("restore-clipboard", true, "restore clipboard when unchanged")
	restoreDelay := fs.Duration("restore-delay", 150*time.Millisecond, "restore delay for QA")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*text) == "" {
		return errors.New("missing required --text")
	}

	clipboard, pasteDriver, err := scenario(*mode)
	if err != nil {
		return err
	}
	injector := inject.NewMacOSPasteInjector(inject.MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      pasteDriver,
		RestoreClipboard: *restoreClipboard,
		RestoreDelay:     *restoreDelay,
		Waiter:           waitForScenario(*mode, clipboard),
	})

	beforeClipboard, beforeErr := clipboard.Read(ctx)
	if beforeErr != nil {
		beforeClipboard = nil
	}
	writeMarker(stdout, "QA_SCENARIO", "app", *appLabel, "mode", *mode)
	writeMarker(stdout, "QA_TEXT", "len", fmt.Sprintf("%d", len(*text)), "sha256", shortHash([]byte(*text)), "value", *text)
	writeMarker(stdout, "QA_CLIPBOARD_BEFORE", "len", fmt.Sprintf("%d", len(beforeClipboard)), "sha256", shortHash(beforeClipboard))

	receipt, injectErr := injector.Insert(ctx, *text)
	afterClipboard, afterErr := clipboard.Read(ctx)
	if afterErr != nil {
		afterClipboard = nil
	}
	writeMarker(stdout, "QA_CLIPBOARD_AFTER", "len", fmt.Sprintf("%d", len(afterClipboard)), "sha256", shortHash(afterClipboard))

	if injectErr != nil {
		var typedErr *inject.InsertError
		if !errors.As(injectErr, &typedErr) {
			return injectErr
		}
		_, _ = fmt.Fprintf(stdout, "EXPECTED_ERROR %s\n", typedErr.Code)
		writeMarker(stdout, "QA_ERROR", "code", string(typedErr.Code), "message", typedErr.UserMessage)
		writeMarker(stdout, "QA_RECOVERY", "action", string(typedErr.Recovery.Action), "volatile", fmt.Sprintf("%t", typedErr.Recovery.Volatile), "failed_len", fmt.Sprintf("%d", len(typedErr.Recovery.FailedText)), "failed_text", typedErr.Recovery.FailedText)
		if *expectErrorCode == "" {
			return typedErr
		}
		if string(typedErr.Code) != *expectErrorCode {
			return fmt.Errorf("expected error code %q, got %q", *expectErrorCode, typedErr.Code)
		}
		return nil
	}

	if *expectErrorCode != "" {
		return fmt.Errorf("expected error code %q, but insert succeeded", *expectErrorCode)
	}

	writeMarker(stdout, "QA_RESULT", "target", receipt.Target, "status", receipt.Status, "restore", string(receipt.RestoreStatus))
	if receipt.WarningCode != "" {
		writeMarker(stdout, "QA_WARNING", "code", receipt.WarningCode, "message", receipt.WarningMessage)
	}
	return nil
}

func scenario(mode string) (inject.Clipboard, inject.PasteDriver, error) {
	switch mode {
	case "real":
		return commandClipboard{}, systemPasteDriver{}, nil
	case "simulate-success":
		return &memoryClipboard{value: []byte("SIMULATED_ORIGINAL_CLIPBOARD")}, successPasteDriver{}, nil
	case "simulate-user-changed":
		return &memoryClipboard{value: []byte("SIMULATED_ORIGINAL_CLIPBOARD")}, successPasteDriver{}, nil
	case "simulate-permission-denied":
		return &memoryClipboard{value: []byte("SIMULATED_ORIGINAL_CLIPBOARD")}, failingPasteDriver{err: inject.ErrAccessibilityPermissionDenied}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported mode %q", mode)
	}
}

func waitForScenario(mode string, clipboard inject.Clipboard) inject.Waiter {
	if mode != "simulate-user-changed" {
		return func(context.Context, time.Duration) error { return nil }
	}
	return func(ctx context.Context, delay time.Duration) error {
		if writer, ok := clipboard.(interface {
			Write(context.Context, []byte) error
		}); ok {
			return writer.Write(ctx, []byte("SIMULATED_USER_CHANGED_CLIPBOARD"))
		}
		return nil
	}
}

func writeMarker(stdout *os.File, label string, fields ...string) {
	parts := []string{label}
	for i := 0; i+1 < len(fields); i += 2 {
		parts = append(parts, fmt.Sprintf("%s=%q", fields[i], fields[i+1]))
	}
	_, _ = fmt.Fprintln(stdout, strings.Join(parts, " "))
}

func shortHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:6])
}

type commandClipboard struct{}

func (commandClipboard) Read(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pbpaste")
	return cmd.Output()
}

func (commandClipboard) Write(ctx context.Context, value []byte) error {
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(string(value))
	return cmd.Run()
}

type systemPasteDriver struct{}

func (systemPasteDriver) Paste(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "osascript", "-e", `tell application "System Events" to keystroke "v" using command down`)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	lower := strings.ToLower(string(output))
	if strings.Contains(lower, "assistive access") ||
		strings.Contains(lower, "not authorized to send apple events") ||
		strings.Contains(lower, "not authorised to send apple events") ||
		strings.Contains(lower, "-1743") {
		return fmt.Errorf("%w: %s", inject.ErrAccessibilityPermissionDenied, strings.TrimSpace(string(output)))
	}
	return fmt.Errorf("osascript paste: %w: %s", err, strings.TrimSpace(string(output)))
}

type successPasteDriver struct{}

func (successPasteDriver) Paste(context.Context) error { return nil }

type failingPasteDriver struct{ err error }

func (f failingPasteDriver) Paste(context.Context) error { return f.err }

type memoryClipboard struct{ value []byte }

func (m *memoryClipboard) Read(context.Context) ([]byte, error) {
	copyOfValue := make([]byte, len(m.value))
	copy(copyOfValue, m.value)
	return copyOfValue, nil
}

func (m *memoryClipboard) Write(_ context.Context, value []byte) error {
	m.value = append(m.value[:0], value...)
	return nil
}
