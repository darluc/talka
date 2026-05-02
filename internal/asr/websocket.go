package asr

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"talka/internal/protocol"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type websocketConn struct {
	conn         net.Conn
	reader       *bufio.Reader
	maskOutbound bool
	writeMu      sync.Mutex
	readTimeout  time.Duration
}

func dialWebSocket(ctx context.Context, rawURL string, timeout time.Duration) (*websocketConn, error) {
	return dialWebSocketWithSubprotocols(ctx, rawURL, timeout, nil)
}

func dialWebSocketWithSubprotocols(ctx context.Context, rawURL string, timeout time.Duration, subprotocols []string) (*websocketConn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if parsed.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported websocket scheme %q", parsed.Scheme)
	}

	host := parsed.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}

	path := parsed.RequestURI()
	if path == "" {
		path = "/"
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	subprotocolHeader := ""
	if len(subprotocols) > 0 {
		subprotocolHeader = fmt.Sprintf("Sec-WebSocket-Protocol: %s\r\n", strings.Join(subprotocols, ", "))
	}

	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n%s\r\n", path, parsed.Host, key, subprotocolHeader)
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		return nil, err
	}

	if response.StatusCode != http.StatusSwitchingProtocols {
		response.Body.Close()
		conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s", response.Status)
	}

	if response.Header.Get("Sec-WebSocket-Accept") != computeAcceptKey(key) {
		response.Body.Close()
		conn.Close()
		return nil, fmt.Errorf("websocket handshake returned unexpected accept key")
	}

	return &websocketConn{conn: conn, reader: reader, maskOutbound: true, readTimeout: timeout}, nil
}

func acceptWebSocket(w http.ResponseWriter, req *http.Request) (*websocketConn, error) {
	if !headerContainsToken(req.Header, "Connection", "Upgrade") || !headerContainsToken(req.Header, "Upgrade", "websocket") {
		http.Error(w, "upgrade required", http.StatusUpgradeRequired)
		return nil, fmt.Errorf("request is not a websocket upgrade")
	}

	key := req.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return nil, fmt.Errorf("missing websocket key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket hijack unsupported", http.StatusInternalServerError)
		return nil, fmt.Errorf("response writer does not support hijacking")
	}

	conn, readerWriter, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	accept := computeAcceptKey(key)
	response := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if _, err := io.WriteString(readerWriter, response); err != nil {
		conn.Close()
		return nil, err
	}
	if err := readerWriter.Flush(); err != nil {
		conn.Close()
		return nil, err
	}

	return &websocketConn{conn: conn, reader: readerWriter.Reader, maskOutbound: false}, nil
}

func (c *websocketConn) Close() error {
	return c.conn.Close()
}

func (c *websocketConn) WriteJSON(msg any) error {
	payload, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

func (c *websocketConn) WriteBinary(payload []byte) error {
	return c.writeFrame(0x2, payload)
}

func readMessage(ctx context.Context, conn *websocketConn) (any, error) {
	payload, err := conn.readFrame(ctx)
	if err != nil {
		return nil, err
	}
	return protocol.Decode(payload)
}

func (c *websocketConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if len(payload) > 65535 {
		return fmt.Errorf("websocket payload too large: %d", len(payload))
	}

	header := []byte{0x80 | opcode}
	length := len(payload)
	maskBit := byte(0)
	if c.maskOutbound {
		maskBit = 0x80
	}

	switch {
	case length < 126:
		header = append(header, maskBit|byte(length))
	default:
		header = append(header, maskBit|126, byte(length>>8), byte(length))
	}

	framePayload := make([]byte, len(payload))
	copy(framePayload, payload)
	if c.maskOutbound {
		maskKey := make([]byte, 4)
		if _, err := rand.Read(maskKey); err != nil {
			return err
		}
		header = append(header, maskKey...)
		for index := range framePayload {
			framePayload[index] ^= maskKey[index%4]
		}
	}

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(framePayload)
	return err
}

func (c *websocketConn) readFrame(ctx context.Context) ([]byte, error) {
	payload, opcode, err := c.readFrameWithOpcode(ctx)
	if err != nil {
		return nil, err
	}
	if opcode != 0x1 {
		return nil, fmt.Errorf("unsupported websocket opcode %d", opcode)
	}
	return payload, nil
}

func (c *websocketConn) readFrameWithOpcode(ctx context.Context) ([]byte, byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return nil, 0, err
		}
	} else if c.readTimeout > 0 {
		if err := c.conn.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
			return nil, 0, err
		}
	} else {
		if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
			return nil, 0, err
		}
	}

	first, err := c.reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	second, err := c.reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}

	opcode := first & 0x0F
	if opcode == 0x8 {
		return nil, 0, io.EOF
	}

	masked := second&0x80 != 0
	payloadLength := int(second & 0x7F)
	switch payloadLength {
	case 126:
		var extended uint16
		if err := binary.Read(c.reader, binary.BigEndian, &extended); err != nil {
			return nil, 0, err
		}
		payloadLength = int(extended)
	case 127:
		return nil, 0, fmt.Errorf("64-bit websocket payloads are not supported")
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, maskKey[:]); err != nil {
			return nil, 0, err
		}
	}

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, 0, err
	}

	if masked {
		for index := range payload {
			payload[index] ^= maskKey[index%4]
		}
	}

	return payload, opcode, nil
}

func computeAcceptKey(key string) string {
	hash := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func headerContainsToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
