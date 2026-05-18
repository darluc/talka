package asr

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

const runtimeLogBufferLimit = 16 * 1024
const maxDynamicPortAttempts = 5

type boundedLogBuffer struct {
	mu       sync.Mutex
	maxBytes int
	buffer   []byte
}

func newBoundedLogBuffer(maxBytes int) *boundedLogBuffer {
	if maxBytes <= 0 {
		maxBytes = runtimeLogBufferLimit
	}
	return &boundedLogBuffer{maxBytes: maxBytes}
}

func (b *boundedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buffer = append(b.buffer, p...)
	if overflow := len(b.buffer) - b.maxBytes; overflow > 0 {
		b.buffer = append([]byte(nil), b.buffer[overflow:]...)
	}
	return len(p), nil
}

func (b *boundedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buffer))
}

func (b *boundedLogBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buffer = nil
}

func firstAvailableLoopbackPort(host string, preferredPort int) (int, bool, error) {
	if preferredPort > 0 {
		available, err := isLoopbackPortAvailable(host, preferredPort)
		if err != nil {
			return 0, false, err
		}
		if available {
			return preferredPort, false, nil
		}
	}

	ephemeral, err := reserveLoopbackPort(host)
	if err != nil {
		return 0, false, err
	}
	return ephemeral, true, nil
}

func nextAvailableLoopbackPort(host string, preferredPort int, attempted map[int]struct{}) (int, error) {
	if attempted == nil {
		attempted = map[int]struct{}{}
	}
	if preferredPort > 0 {
		if _, seen := attempted[preferredPort]; !seen {
			available, err := isLoopbackPortAvailable(host, preferredPort)
			if err != nil {
				return 0, err
			}
			if available {
				return preferredPort, nil
			}
		}
	}

	for tries := 0; tries < maxDynamicPortAttempts*2; tries++ {
		port, err := reserveLoopbackPort(host)
		if err != nil {
			return 0, err
		}
		if _, seen := attempted[port]; seen {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("could not reserve an unused loopback port after %d attempts", maxDynamicPortAttempts*2)
}

func isLoopbackPortAvailable(host string, port int) (bool, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		_ = ln.Close()
		return true, nil
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		text := strings.ToLower(opErr.Err.Error())
		if strings.Contains(text, "address already in use") {
			return false, nil
		}
	}
	return false, err
}

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

func overrideFlagValue(args []string, flagName, value string) []string {
	updated := append([]string(nil), args...)
	for index := 0; index < len(updated); index++ {
		if updated[index] == flagName {
			if index+1 < len(updated) {
				updated[index+1] = value
				return updated
			}
			return append(updated, value)
		}
	}
	return append(updated, flagName, value)
}

func classifyRuntimeStartupFailure(url, diagnostics string, cause error) error {
	return classifyScopedRuntimeStartupFailure("embedded ASR runtime", url, diagnostics, cause)
}

func classifyScopedRuntimeStartupFailure(scope, url, diagnostics string, cause error) error {
	diagnostic := sanitizeRuntimeDiagnostics(diagnostics)
	if diagnostic == "" && cause != nil {
		diagnostic = cause.Error()
	}
	lower := strings.ToLower(diagnostic)
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "ASR runtime"
	}

	switch {
	case strings.Contains(lower, "tlg.fst") || strings.Contains(lower, "model_quant.onnx do not exists") || strings.Contains(lower, "do not exists"):
		return newRuntimeErrorWithDiagnostic(ErrorCodeModelMissing, scope+" model assets are incomplete", diagnostic, cause)
	case strings.Contains(lower, "must exist at"):
		return newRuntimeErrorWithDiagnostic(ErrorCodeRuntimeConfigInvalid, scope+" configuration is invalid", diagnostic, cause)
	case isPortConflictDiagnostic(lower):
		return newRuntimeErrorWithDiagnostic(ErrorCodeRuntimeStartupFailed, fmt.Sprintf("%s could not bind a local port for %s", scope, url), diagnostic, cause)
	default:
		return newRuntimeErrorWithDiagnostic(ErrorCodeRuntimeStartupFailed, fmt.Sprintf("%s could not start for %s", scope, url), diagnostic, cause)
	}
}

func shouldRetryOnDifferentPort(err error) bool {
	if err == nil {
		return false
	}
	diagnostic := strings.ToLower(strings.TrimSpace(RuntimeErrorDiagnosticOf(err)))
	if diagnostic == "" {
		diagnostic = strings.ToLower(strings.TrimSpace(err.Error()))
	}
	return isPortConflictDiagnostic(diagnostic)
}

func isPortConflictDiagnostic(diagnostic string) bool {
	return strings.Contains(diagnostic, "address already in use") ||
		strings.Contains(diagnostic, "port is already allocated") ||
		strings.Contains(diagnostic, "bind: address already in use")
}

func sanitizeRuntimeDiagnostics(diagnostics string) string {
	lines := strings.Split(diagnostics, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case trimmed == "":
			continue
		case lower == "sh: python: command not found":
			continue
		case strings.Contains(lower, "failed to download model from modelscope") && strings.Contains(lower, "you can ignore the errors"):
			continue
		default:
			filtered = append(filtered, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}
