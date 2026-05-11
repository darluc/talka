package inject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	macOSPasteTarget               = "macos_clipboard_paste"
	clipboardRestoreWarningCode    = "clipboard_restore_failed"
	accessibilityRecoveryMessage   = "Talka needs Accessibility or Automation permission before it can paste into other apps."
	pasteFailureRecoveryMessage    = "Talka could not paste the final text into the active app."
	clipboardReadFailureMessage    = "Talka could not read the current clipboard before paste."
	clipboardWriteFailureMessage   = "Talka could not write the final text to the clipboard for paste."
	clipboardRestoreWarningMessage = "Talka pasted successfully but could not restore the prior clipboard."
)

var ErrAccessibilityPermissionDenied = errors.New("accessibility permission denied")

type FailureCode string

const (
	FailureCodeAccessibilityMissing FailureCode = "accessibility_missing"
	FailureCodePasteFailed          FailureCode = "paste_failed"
	FailureCodeClipboardReadFailed  FailureCode = "clipboard_read_failed"
	FailureCodeClipboardWriteFailed FailureCode = "clipboard_write_failed"
)

type RecoveryAction string

const (
	RecoveryActionOpenAccessibilityGuidance RecoveryAction = "open_accessibility_guidance"
	RecoveryActionCopyFailedText            RecoveryAction = "copy_failed_text"
)

type Recovery struct {
	Action     RecoveryAction
	FailedText string
	Volatile   bool
}

type InsertError struct {
	Code        FailureCode
	UserMessage string
	Recovery    Recovery
	Err         error
}

func (e *InsertError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.UserMessage)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.UserMessage, e.Err)
}

func (e *InsertError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Clipboard interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, value []byte) error
}

type PasteDriver interface {
	Paste(ctx context.Context) error
}

type PastePreflightDriver interface {
	Preflight(ctx context.Context) error
}

type Waiter func(ctx context.Context, delay time.Duration) error

type MacOSPasteOptions struct {
	Clipboard        Clipboard
	PasteDriver      PasteDriver
	RestoreClipboard bool
	RestoreDelay     time.Duration
	Waiter           Waiter
}

type MacOSPasteInjector struct {
	clipboard        Clipboard
	pasteDriver      PasteDriver
	restoreClipboard bool
	restoreDelay     time.Duration
	waiter           Waiter
}

func NewMacOSPasteInjector(options MacOSPasteOptions) *MacOSPasteInjector {
	clipboard := options.Clipboard
	if clipboard == nil {
		clipboard = systemClipboard{}
	}
	pasteDriver := options.PasteDriver
	if pasteDriver == nil {
		pasteDriver = defaultPasteDriver()
	}
	waiter := options.Waiter
	if waiter == nil {
		waiter = sleepWaiter
	}
	restoreDelay := options.RestoreDelay
	if restoreDelay <= 0 {
		restoreDelay = 150 * time.Millisecond
	}

	return &MacOSPasteInjector{
		clipboard:        clipboard,
		pasteDriver:      pasteDriver,
		restoreClipboard: options.RestoreClipboard,
		restoreDelay:     restoreDelay,
		waiter:           waiter,
	}
}

func (i *MacOSPasteInjector) Insert(ctx context.Context, text string) (Receipt, error) {
	if preflightDriver, ok := i.pasteDriver.(PastePreflightDriver); ok {
		if err := preflightDriver.Preflight(ctx); err != nil {
			return Receipt{}, insertErrorForPasteFailure(err, text)
		}
	}

	originalClipboard, err := i.clipboard.Read(ctx)
	if err != nil {
		return Receipt{}, &InsertError{
			Code:        FailureCodeClipboardReadFailed,
			UserMessage: clipboardReadFailureMessage,
			Recovery: Recovery{
				Action:     RecoveryActionCopyFailedText,
				FailedText: text,
				Volatile:   true,
			},
			Err: err,
		}
	}

	insertedClipboard := []byte(text)
	if err := i.clipboard.Write(ctx, insertedClipboard); err != nil {
		return Receipt{}, &InsertError{
			Code:        FailureCodeClipboardWriteFailed,
			UserMessage: clipboardWriteFailureMessage,
			Recovery: Recovery{
				Action:     RecoveryActionCopyFailedText,
				FailedText: text,
				Volatile:   true,
			},
			Err: err,
		}
	}

	if err := i.pasteDriver.Paste(ctx); err != nil {
		return Receipt{}, insertErrorForPasteFailure(err, text)
	}

	receipt := Receipt{Target: macOSPasteTarget, Status: "inserted"}
	if !i.restoreClipboard {
		receipt.RestoreStatus = RestoreStatusDisabled
		return receipt, nil
	}

	if err := i.waiter(ctx, i.restoreDelay); err != nil {
		return receipt, nil
	}

	currentClipboard, err := i.clipboard.Read(ctx)
	if err != nil {
		receipt.RestoreStatus = RestoreStatusFailed
		receipt.WarningCode = clipboardRestoreWarningCode
		receipt.WarningMessage = clipboardRestoreWarningMessage
		return receipt, nil
	}
	if !bytes.Equal(currentClipboard, insertedClipboard) {
		receipt.RestoreStatus = RestoreStatusSkippedChanged
		return receipt, nil
	}
	if err := i.clipboard.Write(ctx, originalClipboard); err != nil {
		receipt.RestoreStatus = RestoreStatusFailed
		receipt.WarningCode = clipboardRestoreWarningCode
		receipt.WarningMessage = clipboardRestoreWarningMessage
		return receipt, nil
	}

	receipt.RestoreStatus = RestoreStatusRestored
	return receipt, nil
}

func insertErrorForPasteFailure(err error, text string) *InsertError {
	code := FailureCodePasteFailed
	message := pasteFailureRecoveryMessage
	action := RecoveryActionCopyFailedText
	if errors.Is(err, ErrAccessibilityPermissionDenied) {
		code = FailureCodeAccessibilityMissing
		message = accessibilityRecoveryMessage
		action = RecoveryActionOpenAccessibilityGuidance
	}
	return &InsertError{
		Code:        code,
		UserMessage: message,
		Recovery: Recovery{
			Action:     action,
			FailedText: text,
			Volatile:   true,
		},
		Err: err,
	}
}

type systemClipboard struct{}

func (systemClipboard) Read(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pbpaste")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pbpaste: %w", err)
	}
	return output, nil
}

func (systemClipboard) Write(ctx context.Context, value []byte) error {
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = bytes.NewReader(value)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pbcopy: %w", err)
	}
	return nil
}

type appleScriptPasteDriver struct{}

func (appleScriptPasteDriver) Paste(ctx context.Context) error {
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
		return fmt.Errorf("%w: %s", ErrAccessibilityPermissionDenied, strings.TrimSpace(string(output)))
	}
	return fmt.Errorf("osascript paste: %w: %s", err, strings.TrimSpace(string(output)))
}

func sleepWaiter(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
