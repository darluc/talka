package inject

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pasteBrokerTimeout = 2 * time.Second

type brokerPasteRequest struct {
	Op        string   `json:"op"`
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type brokerPasteResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type brokerPasteDriver struct {
	baseURL string
	client  *http.Client
}

type socketBrokerPasteDriver struct {
	socketPath string
	dialer     net.Dialer
}

func defaultBrokerPasteDriver(socketPath, rawURL string) (PasteDriver, bool) {
	if driver, ok := newSocketBrokerPasteDriver(socketPath); ok {
		return driver, true
	}
	return newBrokerPasteDriver(rawURL)
}

func newSocketBrokerPasteDriver(socketPath string) (PasteDriver, bool) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, false
	}
	return socketBrokerPasteDriver{
		socketPath: socketPath,
		dialer:     net.Dialer{Timeout: pasteBrokerTimeout},
	}, true
}

func newBrokerPasteDriver(rawURL string) (PasteDriver, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, false
	}
	return brokerPasteDriver{
		baseURL: strings.TrimRight(parsed.String(), "/"),
		client:  &http.Client{Timeout: pasteBrokerTimeout},
	}, true
}

func (d socketBrokerPasteDriver) Paste(ctx context.Context) error {
	return d.request(ctx, "paste")
}

func (d socketBrokerPasteDriver) Preflight(ctx context.Context) error {
	return d.request(ctx, "preflight")
}

func (d socketBrokerPasteDriver) KeyPress(ctx context.Context, request KeyPressRequest) error {
	modifiers := make([]string, 0, len(request.Modifiers))
	for _, modifier := range request.Modifiers {
		modifiers = append(modifiers, string(modifier))
	}
	return d.requestEnvelope(ctx, brokerPasteRequest{
		Op:        "key_press",
		Key:       string(request.Key),
		Modifiers: modifiers,
	})
}

func (d socketBrokerPasteDriver) request(ctx context.Context, op string) error {
	return d.requestEnvelope(ctx, brokerPasteRequest{Op: op})
}

func (d socketBrokerPasteDriver) requestEnvelope(ctx context.Context, request brokerPasteRequest) error {
	conn, err := d.dialer.DialContext(ctx, "unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("paste broker socket: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(pasteBrokerTimeout))
	}

	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("paste broker request: %w", err)
	}

	var response brokerPasteResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return fmt.Errorf("paste broker response: %w", err)
	}

	if response.OK {
		return nil
	}
	if response.Error == "accessibility_missing" {
		return fmt.Errorf("%w: %s", ErrAccessibilityPermissionDenied, response.Error)
	}
	if response.Error == "" {
		response.Error = "unknown_error"
	}
	return fmt.Errorf("paste broker returned error: %s", response.Error)
}

func (d brokerPasteDriver) Paste(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/paste", nil)
	if err != nil {
		return err
	}

	response, err := d.client.Do(request)
	if err != nil {
		return fmt.Errorf("paste broker: %w", err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
	if response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrAccessibilityPermissionDenied, strings.TrimSpace(string(body)))
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("paste broker returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
