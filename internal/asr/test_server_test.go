package asr

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func startFakeRuntimeServer(t *testing.T, runtime *FakeRuntime) (string, func()) {
	t.Helper()
	server := httptest.NewServer(runtime.Handler())
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	return wsURL, server.Close
}
