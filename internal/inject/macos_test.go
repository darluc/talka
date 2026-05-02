package inject

import (
	"context"
	"errors"
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
	calls int
	err   error
}

func (s *stubPasteDriver) Paste(context.Context) error {
	s.calls++
	return s.err
}

func noopWaiter(context.Context, time.Duration) error {
	return nil
}
