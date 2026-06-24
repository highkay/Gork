package webui

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/platform/config"
)

const webUIWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
const defaultWebUIWebSocketMaxMessageBytes = 1 << 20

type webUIWebSocket struct {
	conn            net.Conn
	reader          *bufio.Reader
	mu              sync.Mutex
	closeOnce       sync.Once
	release         func()
	maxMessageBytes int
	readTimeout     time.Duration
	writeTimeout    time.Duration
}

type webUIWebSocketOptions struct {
	MaxMessageBytes     int
	MaxConnections      int
	MaxConnectionsPerIP int
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	AllowedOrigins      []any
}

type webUIWebSocketConnectionLimiter struct {
	mu    sync.Mutex
	total int
	byIP  map[string]int
}

var (
	webUIWebSocketOptionsProvider = defaultWebUIWebSocketOptions
	webUIWebSocketLimiter         = newWebUIWebSocketConnectionLimiter()
)

func acceptWebUIWebSocket(w http.ResponseWriter, r *http.Request) (*webUIWebSocket, error) {
	if !webUIWebSocketUpgradeRequest(r) {
		http.Error(w, "Bad websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("bad websocket upgrade")
	}
	options := webUIWebSocketOptionsProvider()
	if !webUIWebSocketOriginAllowed(r, options) {
		http.Error(w, "Forbidden websocket origin", http.StatusForbidden)
		return nil, errors.New("forbidden websocket origin")
	}
	release, ok := webUIWebSocketLimiter.Acquire(webUIWebSocketRemoteIP(r), options)
	if !ok {
		http.Error(w, "Too many websocket connections", http.StatusTooManyRequests)
		return nil, errors.New("too many websocket connections")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		release()
		http.Error(w, "Websocket unsupported", http.StatusInternalServerError)
		return nil, errors.New("websocket unsupported")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		release()
		return nil, err
	}
	accept := webUIWebSocketAccept(r.Header.Get("Sec-WebSocket-Key"))
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n")
	_, _ = fmt.Fprintf(rw, "Upgrade: websocket\r\nConnection: Upgrade\r\n")
	_, _ = fmt.Fprintf(rw, "Sec-WebSocket-Accept: %s\r\n", accept)
	for key, values := range w.Header() {
		for _, value := range values {
			_, _ = fmt.Fprintf(rw, "%s: %s\r\n", key, value)
		}
	}
	_, _ = fmt.Fprintf(rw, "\r\n")
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		release()
		return nil, err
	}
	return &webUIWebSocket{
		conn:            conn,
		reader:          rw.Reader,
		release:         release,
		maxMessageBytes: options.MaxMessageBytes,
		readTimeout:     options.ReadTimeout,
		writeTimeout:    options.WriteTimeout,
	}, nil
}

func webUIWebSocketUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key")) != "" &&
		r.Header.Get("Sec-WebSocket-Version") == "13"
}

func webUIWebSocketAccept(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key) + webUIWebSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func defaultWebUIWebSocketOptions() webUIWebSocketOptions {
	return webUIWebSocketOptions{
		MaxMessageBytes:     intFromAny(config.GetConfig("security.websocket.max_message_bytes", defaultWebUIWebSocketMaxMessageBytes), defaultWebUIWebSocketMaxMessageBytes),
		MaxConnections:      intFromAny(config.GetConfig("security.websocket.max_connections", 128), 128),
		MaxConnectionsPerIP: intFromAny(config.GetConfig("security.websocket.max_connections_per_ip", 16), 16),
		ReadTimeout:         time.Duration(intFromAny(config.GetConfig("security.websocket.read_timeout_seconds", 60), 60)) * time.Second,
		WriteTimeout:        time.Duration(intFromAny(config.GetConfig("security.websocket.write_timeout_seconds", 15), 15)) * time.Second,
		AllowedOrigins:      config.GlobalConfig.GetList("security.cors.web_allowed_origins", nil),
	}
}

func newWebUIWebSocketConnectionLimiter() *webUIWebSocketConnectionLimiter {
	return &webUIWebSocketConnectionLimiter{byIP: map[string]int{}}
}

func (l *webUIWebSocketConnectionLimiter) Acquire(ip string, options webUIWebSocketOptions) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if options.MaxConnections > 0 && l.total >= options.MaxConnections {
		return nil, false
	}
	if options.MaxConnectionsPerIP > 0 && l.byIP[ip] >= options.MaxConnectionsPerIP {
		return nil, false
	}
	l.total++
	l.byIP[ip]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.total--
		l.byIP[ip]--
		if l.byIP[ip] <= 0 {
			delete(l.byIP, ip)
		}
	}, true
}

func webUIWebSocketOriginAllowed(r *http.Request, options webUIWebSocketOptions) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || webUIWebSocketSameOrigin(r, origin) {
		return true
	}
	for _, value := range options.AllowedOrigins {
		if strings.TrimSpace(fmt.Sprint(value)) == origin {
			return true
		}
	}
	return false
}

func webUIWebSocketSameOrigin(r *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func webUIWebSocketRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (ws *webUIWebSocket) ReadText() (string, error) {
	for {
		opcode, payload, err := ws.readFrame()
		if err != nil {
			return "", err
		}
		switch opcode {
		case 1:
			return string(payload), nil
		case 8:
			return "", io.EOF
		case 9:
			_ = ws.writeFrame(10, payload)
		}
	}
}

func (ws *webUIWebSocket) WriteJSON(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return ws.WriteText(string(raw))
}

func (ws *webUIWebSocket) WriteText(text string) error {
	return ws.writeFrame(1, []byte(text))
}

func (ws *webUIWebSocket) Close() error {
	var err error
	ws.closeOnce.Do(func() {
		_ = ws.writeFrame(8, nil)
		err = ws.conn.Close()
		if ws.release != nil {
			ws.release()
		}
	})
	return err
}

func (ws *webUIWebSocket) readFrame() (byte, []byte, error) {
	if ws.readTimeout > 0 {
		_ = ws.conn.SetReadDeadline(time.Now().Add(ws.readTimeout))
	}
	first, err := ws.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := ws.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length, err := ws.readFrameLength(second)
	if err != nil {
		return 0, nil, err
	}
	if ws.maxMessageBytes > 0 && length > ws.maxMessageBytes {
		return 0, nil, errors.New("websocket message too large")
	}
	mask, err := ws.readMask(second)
	if err != nil {
		return 0, nil, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(ws.reader, payload); err != nil {
		return 0, nil, err
	}
	if mask != nil {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return first & 0x0f, payload, nil
}

func (ws *webUIWebSocket) readFrameLength(second byte) (int, error) {
	length := int(second & 0x7f)
	if length < 126 {
		return length, nil
	}
	raw := make([]byte, 2)
	if length == 127 {
		raw = make([]byte, 8)
	}
	if _, err := io.ReadFull(ws.reader, raw); err != nil {
		return 0, err
	}
	if length == 126 {
		return int(binary.BigEndian.Uint16(raw)), nil
	}
	value := binary.BigEndian.Uint64(raw)
	if value > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("websocket frame too large")
	}
	return int(value), nil
}

func (ws *webUIWebSocket) readMask(second byte) ([]byte, error) {
	if second&0x80 == 0 {
		return nil, nil
	}
	mask := make([]byte, 4)
	_, err := io.ReadFull(ws.reader, mask)
	return mask, err
}

func (ws *webUIWebSocket) writeFrame(opcode byte, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.writeTimeout > 0 {
		_ = ws.conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))
	}
	header := webUIWebSocketFrameHeader(opcode, len(payload))
	if _, err := ws.conn.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := ws.conn.Write(payload)
	return err
}

func webUIWebSocketFrameHeader(opcode byte, length int) []byte {
	header := []byte{0x80 | opcode}
	if length < 126 {
		return append(header, byte(length))
	}
	if length <= 65535 {
		out := append(header, 126, 0, 0)
		binary.BigEndian.PutUint16(out[2:], uint16(length))
		return out
	}
	out := append(header, 127, 0, 0, 0, 0, 0, 0, 0, 0)
	binary.BigEndian.PutUint64(out[2:], uint64(length))
	return out
}
