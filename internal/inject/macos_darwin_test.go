//go:build darwin

package inject

import "testing"

func TestMacOSPasteInjectorDefaultsToNativePasteDriverOnDarwin(t *testing.T) {
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard: &memoryClipboard{value: []byte("original clipboard")},
	})

	if _, ok := injector.pasteDriver.(appleScriptPasteDriver); ok {
		t.Fatal("default paste driver = appleScriptPasteDriver, want native macOS event driver")
	}
}

func TestMacOSPasteInjectorUsesBrokerWhenConfiguredOnDarwin(t *testing.T) {
	t.Setenv("TALKA_PASTE_BROKER_SOCKET", "/tmp/talka-paste.sock")

	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard: &memoryClipboard{value: []byte("original clipboard")},
	})

	if _, ok := injector.pasteDriver.(socketBrokerPasteDriver); !ok {
		t.Fatalf("default paste driver = %T, want socketBrokerPasteDriver", injector.pasteDriver)
	}
}
