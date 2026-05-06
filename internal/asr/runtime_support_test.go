package asr

import (
	"strings"
	"testing"
)

func TestSanitizeRuntimeDiagnosticsRemovesBenignDownloadNoise(t *testing.T) {
	raw := `
sh: python: command not found
I20260506 00:37:20.599792 funasr-wss-server-2pass.cpp:238] Failed to download model from modelscope. If you set local vad model path, you can ignore the errors.
I20260506 00:37:20.612115 funasr-wss-server-2pass.cpp:523] hotword path: /tmp/hotwords.txt
F20260506 00:37:20.699792 funasr-wss-server-2pass.cpp:999] bind: Address already in use
`

	got := sanitizeRuntimeDiagnostics(raw)
	if got == "" {
		t.Fatal("sanitizeRuntimeDiagnostics() = empty, want meaningful diagnostics")
	}
	if got == raw {
		t.Fatal("sanitizeRuntimeDiagnostics() did not remove benign noise")
	}
	for _, unwanted := range []string{
		"python: command not found",
		"Failed to download model from modelscope",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("sanitizeRuntimeDiagnostics() unexpectedly kept %q in %q", unwanted, got)
		}
	}
	if !strings.Contains(got, "hotword path") || !strings.Contains(got, "Address already in use") {
		t.Fatalf("sanitizeRuntimeDiagnostics() = %q, want relevant diagnostics retained", got)
	}
}
