//go:build darwin

package inject

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static int talka_ax_trusted(void) {
	return AXIsProcessTrusted() ? 1 : 0;
}

static void talka_post_command_v(void) {
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	CGEventRef keyDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)9, true);
	CGEventRef keyUp = CGEventCreateKeyboardEvent(source, (CGKeyCode)9, false);

	CGEventSetFlags(keyDown, kCGEventFlagMaskCommand);
	CGEventSetFlags(keyUp, kCGEventFlagMaskCommand);

	CGEventPost(kCGHIDEventTap, keyDown);
	CGEventPost(kCGHIDEventTap, keyUp);

	CFRelease(keyDown);
	CFRelease(keyUp);
	CFRelease(source);
}

static void talka_post_key(CGKeyCode key, CGEventFlags flags) {
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	CGEventRef keyDown = CGEventCreateKeyboardEvent(source, key, true);
	CGEventRef keyUp = CGEventCreateKeyboardEvent(source, key, false);

	CGEventSetFlags(keyDown, flags);
	CGEventSetFlags(keyUp, flags);

	CGEventPost(kCGHIDEventTap, keyDown);
	CGEventPost(kCGHIDEventTap, keyUp);

	CFRelease(keyDown);
	CFRelease(keyUp);
	CFRelease(source);
}
*/
import "C"
import "context"
import "fmt"
import "os"

type nativePasteDriver struct{}

func defaultPasteDriver() PasteDriver {
	if driver, ok := defaultBrokerPasteDriver(
		os.Getenv("TALKA_PASTE_BROKER_SOCKET"),
		os.Getenv("TALKA_PASTE_BROKER_URL"),
	); ok {
		return driver
	}
	return nativePasteDriver{}
}

func (nativePasteDriver) Preflight(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if C.talka_ax_trusted() == 0 {
		return ErrAccessibilityPermissionDenied
	}

	return nil
}

func (driver nativePasteDriver) Paste(ctx context.Context) error {
	if err := driver.Preflight(ctx); err != nil {
		return err
	}

	C.talka_post_command_v()
	return nil
}

func (driver nativePasteDriver) KeyPress(ctx context.Context, request KeyPressRequest) error {
	if err := driver.Preflight(ctx); err != nil {
		return err
	}

	var key C.CGKeyCode
	switch request.Key {
	case KeyEnter:
		key = 36
	default:
		return fmt.Errorf("unsupported key press %q", request.Key)
	}

	var flags C.CGEventFlags
	for _, modifier := range request.Modifiers {
		switch modifier {
		case KeyModifierCommand:
			flags |= C.kCGEventFlagMaskCommand
		case KeyModifierAlt:
			flags |= C.kCGEventFlagMaskAlternate
		case KeyModifierShift:
			flags |= C.kCGEventFlagMaskShift
		default:
			return fmt.Errorf("unsupported key modifier %q", modifier)
		}
	}

	C.talka_post_key(key, flags)
	return nil
}
