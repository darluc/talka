package asr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpstreamProviderTranscribesFunASRSessionDirectly(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := acceptWebSocket(w, req)
		if err != nil {
			t.Fatalf("acceptWebSocket() error = %v", err)
		}
		defer conn.Close()

		payload, opcode, err := conn.readFrameWithOpcode(context.Background())
		if err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("readFrameWithOpcode(start) error = %v", err)
		}
		if opcode != 0x1 {
			t.Fatalf("start opcode = %d, want text", opcode)
		}

		var start funASRStartMessage
		if err := json.Unmarshal(payload, &start); err != nil {
			t.Fatalf("Unmarshal(start) error = %v", err)
		}
		if got, want := start.Mode, "2pass"; got != want {
			t.Fatalf("start.Mode = %q, want %q", got, want)
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(audio) error = %v", err)
		} else if opcode != 0x2 {
			t.Fatalf("audio opcode = %d, want binary", opcode)
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-online", "text": "你好", "is_final": false}); err != nil {
			t.Fatalf("WriteJSON(partial) error = %v", err)
		}

		if _, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(stop) error = %v", err)
		} else if opcode != 0x1 {
			t.Fatalf("stop opcode = %d, want text", opcode)
		}

		if payload, opcode, err := conn.readFrameWithOpcode(context.Background()); err != nil {
			t.Fatalf("readFrameWithOpcode(flush) error = %v", err)
		} else if opcode != 0x2 {
			t.Fatalf("flush opcode = %d, want binary", opcode)
		} else if len(payload) != 0 {
			t.Fatalf("flush payload length = %d, want 0", len(payload))
		}

		if err := conn.WriteJSON(map[string]any{"mode": "2pass-offline", "text": "你好，世界", "is_final": true}); err != nil {
			t.Fatalf("WriteJSON(final) error = %v", err)
		}
	}))
	defer upstream.Close()

	provider := NewUpstreamProvider(nil, UpstreamProviderConfig{
		URL:     websocketURLFromHTTP(t, upstream.URL),
		Mode:    "twopass",
		Timeout: 2 * time.Second,
	})

	result, err := provider.Transcribe(context.Background(), Request{
		Metadata: DefaultAudioMetadata(),
		Frames:   [][]byte{make([]byte, 640)},
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got, want := len(result.Partials), 1; got != want {
		t.Fatalf("len(Partials) = %d, want %d", got, want)
	}
	if got, want := result.Partials[0].Text, "你好"; got != want {
		t.Fatalf("Partials[0].Text = %q, want %q", got, want)
	}
	if got, want := result.Transcript, "你好，世界"; got != want {
		t.Fatalf("Transcript = %q, want %q", got, want)
	}
}
