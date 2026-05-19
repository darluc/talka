package inject

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMacOSPasteInjectorInsertReportsSuccessWithoutRestore(t *testing.T) {
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	paster := &stubPasteDriver{}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      paster,
		RestoreClipboard: false,
	})

	receipt, err := injector.Insert(context.Background(), "Talka 测试文本。")
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if got, want := receipt.Target, "macos_clipboard_paste"; got != want {
		t.Fatalf("Target = %q, want %q", got, want)
	}
	if got, want := receipt.Status, "inserted"; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := receipt.RestoreStatus, RestoreStatusDisabled; got != want {
		t.Fatalf("RestoreStatus = %q, want %q", got, want)
	}
	if got, want := string(clipboard.value), "Talka 测试文本。"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	if paster.calls != 1 {
		t.Fatalf("Paste() calls = %d, want 1", paster.calls)
	}
}

func TestMacOSPasteInjectorInsertReturnsAccessibilityRecovery(t *testing.T) {
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      &stubPasteDriver{err: ErrAccessibilityPermissionDenied},
		RestoreClipboard: true,
	})

	_, err := injector.Insert(context.Background(), "Talka 测试文本。")
	if err == nil {
		t.Fatal("Insert() error = nil, want accessibility failure")
	}

	var insertErr *InsertError
	if !errors.As(err, &insertErr) {
		t.Fatalf("Insert() error type = %T, want *InsertError", err)
	}
	if got, want := insertErr.Code, FailureCodeAccessibilityMissing; got != want {
		t.Fatalf("Code = %q, want %q", got, want)
	}
	if insertErr.UserMessage == "" {
		t.Fatal("UserMessage is empty")
	}
	if got, want := insertErr.Recovery.Action, RecoveryActionOpenAccessibilityGuidance; got != want {
		t.Fatalf("Recovery.Action = %q, want %q", got, want)
	}
	if got, want := insertErr.Recovery.FailedText, "Talka 测试文本。"; got != want {
		t.Fatalf("Recovery.FailedText = %q, want %q", got, want)
	}
	if !insertErr.Recovery.Volatile {
		t.Fatal("Recovery.Volatile = false, want true")
	}
	if got, want := string(clipboard.value), "Talka 测试文本。"; got != want {
		t.Fatalf("clipboard = %q, want failed text to remain available", got)
	}
}

func TestMacOSPasteInjectorPreflightFailureDoesNotChangeClipboard(t *testing.T) {
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      &stubPasteDriver{preflightErr: ErrAccessibilityPermissionDenied},
		RestoreClipboard: true,
	})

	_, err := injector.Insert(context.Background(), "Talka 测试文本。")
	if err == nil {
		t.Fatal("Insert() error = nil, want accessibility failure")
	}

	var insertErr *InsertError
	if !errors.As(err, &insertErr) {
		t.Fatalf("Insert() error type = %T, want *InsertError", err)
	}
	if got, want := insertErr.Code, FailureCodeAccessibilityMissing; got != want {
		t.Fatalf("Code = %q, want %q", got, want)
	}
	if got, want := string(clipboard.value), "original clipboard"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestMacOSPasteInjectorInsertReturnsPasteFailureRecovery(t *testing.T) {
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      &stubPasteDriver{err: errors.New("paste command failed")},
		RestoreClipboard: true,
	})

	_, err := injector.Insert(context.Background(), "Talka 测试文本。")
	if err == nil {
		t.Fatal("Insert() error = nil, want paste failure")
	}

	var insertErr *InsertError
	if !errors.As(err, &insertErr) {
		t.Fatalf("Insert() error type = %T, want *InsertError", err)
	}
	if got, want := insertErr.Code, FailureCodePasteFailed; got != want {
		t.Fatalf("Code = %q, want %q", got, want)
	}
	if got, want := insertErr.Recovery.Action, RecoveryActionCopyFailedText; got != want {
		t.Fatalf("Recovery.Action = %q, want %q", got, want)
	}
	if got, want := insertErr.Recovery.FailedText, "Talka 测试文本。"; got != want {
		t.Fatalf("Recovery.FailedText = %q, want %q", got, want)
	}
}

func TestMacOSPasteInjectorInsertRestoresClipboardWhenUnchanged(t *testing.T) {
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      &stubPasteDriver{},
		RestoreClipboard: true,
		RestoreDelay:     10 * time.Millisecond,
		Waiter:           noopWaiter,
	})

	receipt, err := injector.Insert(context.Background(), "Talka 测试文本。")
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if got, want := receipt.RestoreStatus, RestoreStatusRestored; got != want {
		t.Fatalf("RestoreStatus = %q, want %q", got, want)
	}
	if got, want := string(clipboard.value), "original clipboard"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestMacOSPasteInjectorInsertSkipsRestoreWhenUserChangesClipboard(t *testing.T) {
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:        clipboard,
		PasteDriver:      &stubPasteDriver{},
		RestoreClipboard: true,
		RestoreDelay:     10 * time.Millisecond,
		Waiter: func(context.Context, time.Duration) error {
			clipboard.value = []byte("user changed clipboard")
			return nil
		},
	})

	receipt, err := injector.Insert(context.Background(), "Talka 测试文本。")
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if got, want := receipt.RestoreStatus, RestoreStatusSkippedChanged; got != want {
		t.Fatalf("RestoreStatus = %q, want %q", got, want)
	}
	if got, want := string(clipboard.value), "user changed clipboard"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestMacOSPasteInjectorDelegatesKeyPressToPasteDriver(t *testing.T) {
	driver := &stubPasteDriver{}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		PasteDriver: driver,
	})

	err := injector.KeyPress(context.Background(), KeyPressRequest{
		Key:       KeyEnter,
		Modifiers: []KeyModifier{KeyModifierCommand, KeyModifierShift},
	})
	if err != nil {
		t.Fatalf("KeyPress() error = %v", err)
	}

	if got, want := driver.keyPressCalls, 1; got != want {
		t.Fatalf("keyPressCalls = %d, want %d", got, want)
	}
	if got, want := driver.keyPressRequests[0].Key, KeyEnter; got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
	if got, want := driver.keyPressRequests[0].Modifiers, []KeyModifier{KeyModifierCommand, KeyModifierShift}; !keyModifiersEqual(got, want) {
		t.Fatalf("Modifiers = %v, want %v", got, want)
	}
}

func TestSystemClipboardWriteForcesUTF8LocaleForPBcopy(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "pbcopy.env")
	stdinPath := filepath.Join(tempDir, "pbcopy.stdin")
	pbcopyPath := filepath.Join(tempDir, "pbcopy")
	script := "#!/bin/sh\n" +
		"printf 'LANG=%s\\nLC_CTYPE=%s\\n' \"$LANG\" \"$LC_CTYPE\" > \"$PBCOPY_ENV_PATH\"\n" +
		"/bin/cat > \"$PBCOPY_STDIN_PATH\"\n"
	if err := os.WriteFile(pbcopyPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(pbcopy) error = %v", err)
	}
	t.Setenv("PATH", tempDir)
	t.Setenv("PBCOPY_ENV_PATH", envPath)
	t.Setenv("PBCOPY_STDIN_PATH", stdinPath)
	t.Setenv("LANG", "C")
	t.Setenv("LC_CTYPE", "C")

	text := "从前有座山，山上有座庙，庙里有个老和尚。"
	if err := (systemClipboard{}).Write(context.Background(), []byte(text)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	env := string(envBytes)
	if !strings.Contains(env, "LANG=en_US.UTF-8\n") {
		t.Fatalf("pbcopy LANG env = %q, want en_US.UTF-8", env)
	}
	if !strings.Contains(env, "LC_CTYPE=en_US.UTF-8\n") {
		t.Fatalf("pbcopy LC_CTYPE env = %q, want en_US.UTF-8", env)
	}

	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("ReadFile(stdin) error = %v", err)
	}
	if got := string(stdinBytes); got != text {
		t.Fatalf("pbcopy stdin = %q, want %q", got, text)
	}
}

type memoryClipboard struct {
	value []byte
	err   error
}

func (m *memoryClipboard) Read(context.Context) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	copyOfValue := make([]byte, len(m.value))
	copy(copyOfValue, m.value)
	return copyOfValue, nil
}

func (m *memoryClipboard) Write(_ context.Context, value []byte) error {
	if m.err != nil {
		return m.err
	}
	m.value = append(m.value[:0], value...)
	return nil
}

type stubPasteDriver struct {
	calls            int
	err              error
	preflightErr     error
	keyPressCalls    int
	keyPressRequests []KeyPressRequest
	keyPressErr      error
}

func (s *stubPasteDriver) Preflight(context.Context) error {
	return s.preflightErr
}

func (s *stubPasteDriver) Paste(context.Context) error {
	s.calls++
	return s.err
}

func (s *stubPasteDriver) KeyPress(_ context.Context, request KeyPressRequest) error {
	s.keyPressCalls++
	s.keyPressRequests = append(s.keyPressRequests, request)
	return s.keyPressErr
}

func keyModifiersEqual(a, b []KeyModifier) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func noopWaiter(context.Context, time.Duration) error {
	return nil
}
