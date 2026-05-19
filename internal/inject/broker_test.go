package inject

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
)

func TestSocketBrokerPasteDriverSendsPasteRequest(t *testing.T) {
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	requests := make(chan brokerPasteRequest, 1)
	go serveSocketBroker(t, listener, func(request brokerPasteRequest) brokerPasteResponse {
		requests <- request
		return brokerPasteResponse{OK: true}
	})

	driver, ok := newSocketBrokerPasteDriver(socketPath)
	if !ok {
		t.Fatal("newSocketBrokerPasteDriver returned ok=false")
	}

	if err := driver.Paste(context.Background()); err != nil {
		t.Fatalf("Paste returned error: %v", err)
	}

	request := <-requests
	if request.Op != "paste" {
		t.Fatalf("request op = %q, want paste", request.Op)
	}
}

func TestSocketBrokerPasteDriverMapsAccessibilityError(t *testing.T) {
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go serveSocketBroker(t, listener, func(request brokerPasteRequest) brokerPasteResponse {
		return brokerPasteResponse{OK: false, Error: "accessibility_missing"}
	})

	driver, ok := newSocketBrokerPasteDriver(socketPath)
	if !ok {
		t.Fatal("newSocketBrokerPasteDriver returned ok=false")
	}

	err = driver.Paste(context.Background())
	if !errors.Is(err, ErrAccessibilityPermissionDenied) {
		t.Fatalf("Paste error = %v, want ErrAccessibilityPermissionDenied", err)
	}
}

func TestSocketBrokerPasteDriverSendsKeyPressRequest(t *testing.T) {
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	requests := make(chan brokerPasteRequest, 1)
	go serveSocketBroker(t, listener, func(request brokerPasteRequest) brokerPasteResponse {
		requests <- request
		return brokerPasteResponse{OK: true}
	})

	driver, ok := newSocketBrokerPasteDriver(socketPath)
	if !ok {
		t.Fatal("newSocketBrokerPasteDriver returned ok=false")
	}

	keyDriver, ok := driver.(KeyPressDriver)
	if !ok {
		t.Fatalf("broker driver = %T, want KeyPressDriver", driver)
	}

	err = keyDriver.KeyPress(context.Background(), KeyPressRequest{
		Key:       KeyEnter,
		Modifiers: []KeyModifier{KeyModifierCommand, KeyModifierAlt, KeyModifierShift},
	})
	if err != nil {
		t.Fatalf("KeyPress returned error: %v", err)
	}

	request := <-requests
	if request.Op != "key_press" {
		t.Fatalf("request op = %q, want key_press", request.Op)
	}
	if request.Key != "enter" {
		t.Fatalf("request key = %q, want enter", request.Key)
	}
	if got, want := request.Modifiers, []string{"cmd", "alt", "shift"}; !stringSlicesEqual(got, want) {
		t.Fatalf("request modifiers = %v, want %v", got, want)
	}
}

func TestMacOSPasteInjectorSocketBrokerPreflightFailureDoesNotChangeClipboard(t *testing.T) {
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	requests := make(chan brokerPasteRequest, 1)
	go serveSocketBroker(t, listener, func(request brokerPasteRequest) brokerPasteResponse {
		requests <- request
		return brokerPasteResponse{OK: false, Error: "accessibility_missing"}
	})

	driver, ok := newSocketBrokerPasteDriver(socketPath)
	if !ok {
		t.Fatal("newSocketBrokerPasteDriver returned ok=false")
	}
	clipboard := &memoryClipboard{value: []byte("original clipboard")}
	injector := NewMacOSPasteInjector(MacOSPasteOptions{
		Clipboard:   clipboard,
		PasteDriver: driver,
	})

	_, err = injector.Insert(context.Background(), "Talka 测试文本。")
	if !errors.Is(err, ErrAccessibilityPermissionDenied) {
		t.Fatalf("Insert error = %v, want ErrAccessibilityPermissionDenied", err)
	}
	if got, want := string(clipboard.value), "original clipboard"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}

	request := <-requests
	if request.Op != "preflight" {
		t.Fatalf("request op = %q, want preflight", request.Op)
	}
}

func TestSocketBrokerPasteDriverRejectsBlankPath(t *testing.T) {
	driver, ok := newSocketBrokerPasteDriver(" \t\n ")
	if ok || driver != nil {
		t.Fatalf("newSocketBrokerPasteDriver blank path = (%T, %v), want nil false", driver, ok)
	}
}

func serveSocketBroker(t *testing.T, listener net.Listener, respond func(brokerPasteRequest) brokerPasteResponse) {
	t.Helper()

	conn, err := listener.Accept()
	if err != nil {
		if errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed) {
			return
		}
		t.Errorf("Accept: %v", err)
		return
	}
	defer conn.Close()

	var request brokerPasteRequest
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&request); err != nil {
		t.Errorf("Decode request: %v", err)
		return
	}
	if err := json.NewEncoder(conn).Encode(respond(request)); err != nil {
		t.Errorf("Encode response: %v", err)
	}
}

func shortSocketPath(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "talka-paste-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir + "/paste.sock"
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
