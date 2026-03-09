package router

import (
	"bufio"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebSocketConfig configures WebSocket proxy behavior
type WebSocketConfig struct {
	Enabled        bool          `yaml:"enabled"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	MaxMessageSize int64         `yaml:"max_message_size"`
}

// WebSocketProxy handles WebSocket upgrade requests and proxies bidirectional
// frames between client and upstream LLM provider.
//
// P3 #22: Supports Codex v2 WebSocket transport (responses_websockets_v2).
// - Detects `Connection: Upgrade` + `Upgrade: websocket` headers
// - Hijacks the HTTP connection
// - Dials upstream WebSocket endpoint
// - Bidirectionally copies frames between client ↔ upstream
// - Supports Codex OAuth rewrite (chatgpt.com endpoint)
type WebSocketProxy struct {
	config WebSocketConfig
}

// NewWebSocketProxy creates a new WebSocket proxy
func NewWebSocketProxy(cfg WebSocketConfig) *WebSocketProxy {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Minute
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Minute
	}
	if cfg.MaxMessageSize == 0 {
		cfg.MaxMessageSize = 10 << 20 // 10MB
	}
	return &WebSocketProxy{config: cfg}
}

// IsWebSocketUpgrade checks if the request is a WebSocket upgrade
func IsWebSocketUpgrade(r *http.Request) bool {
	connHeader := strings.ToLower(r.Header.Get("Connection"))
	return strings.Contains(connHeader, "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// ServeHTTP handles a WebSocket upgrade request by:
// 1. Hijacking the client connection
// 2. Establishing an upstream WebSocket connection via raw TCP/TLS
// 3. Forwarding the upgrade handshake
// 4. Bidirectionally proxying all frames
func (wsp *WebSocketProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, targetURL *url.URL) {
	slog.Info("websocket upgrade",
		"path", r.URL.Path,
		"target", targetURL.String())

	// Hijack the client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		slog.Error("websocket: ResponseWriter does not support hijacking")
		http.Error(w, `{"error":"websocket_not_supported"}`, http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		slog.Error("websocket: hijack failed", "error", err)
		return
	}
	defer clientConn.Close()


	// Connect to upstream
	upstreamConn, err := dialUpstream(targetURL)
	if err != nil {
		slog.Error("websocket: upstream dial failed", "target", targetURL.Host, "error", err)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer upstreamConn.Close()

	// Build and forward upgrade request to upstream
	upstreamReq := buildUpgradeRequest(r, targetURL)
	if err := upstreamReq.Write(upstreamConn); err != nil {
		slog.Error("websocket: failed to send upgrade", "error", err)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}

	// Read upstream upgrade response
	upstreamBuf := bufio.NewReader(upstreamConn)
	resp, err := http.ReadResponse(upstreamBuf, upstreamReq)
	if err != nil {
		slog.Error("websocket: failed to read upstream response", "error", err)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}

	// Forward the response to client
	if err := resp.Write(clientConn); err != nil {
		slog.Error("websocket: failed to forward response to client", "error", err)
		return
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		slog.Warn("websocket: upstream rejected upgrade",
			"status", resp.StatusCode)
		return
	}

	slog.Info("websocket: tunnel established",
		"path", r.URL.Path,
		"upstream", targetURL.Host)

	// Bidirectional proxy: client ↔ upstream
	done := make(chan struct{}, 2)

	// Client → Upstream
	go func() {
		defer func() { done <- struct{}{} }()
		// Flush any buffered data from hijack
		if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
			io.CopyN(upstreamConn, clientBuf, int64(clientBuf.Reader.Buffered()))
		}
		wsCopy(upstreamConn, clientConn, wsp.config.ReadTimeout)
	}()

	// Upstream → Client
	go func() {
		defer func() { done <- struct{}{} }()
		// Flush any buffered data from upstream
		if upstreamBuf.Buffered() > 0 {
			io.CopyN(clientConn, upstreamBuf, int64(upstreamBuf.Buffered()))
		}
		wsCopy(clientConn, upstreamConn, wsp.config.ReadTimeout)
	}()

	// Wait for either direction to close
	<-done
	slog.Info("websocket: connection closed", "path", r.URL.Path)
}

// dialUpstream establishes a TCP or TLS connection to the upstream WebSocket server
func dialUpstream(target *url.URL) (net.Conn, error) {
	host := target.Host
	useTLS := target.Scheme == "https" || target.Scheme == "wss"
	if !strings.Contains(host, ":") {
		if useTLS {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	if useTLS {
		return tls.DialWithDialer(
			&net.Dialer{Timeout: 10 * time.Second},
			"tcp", host,
			&tls.Config{ServerName: target.Hostname()},
		)
	}
	return net.DialTimeout("tcp", host, 10*time.Second)
}

// buildUpgradeRequest constructs the HTTP upgrade request for upstream
func buildUpgradeRequest(original *http.Request, target *url.URL) *http.Request {
	req := &http.Request{
		Method: original.Method,
		URL:    &url.URL{Path: original.URL.Path, RawQuery: original.URL.RawQuery},
		Header: make(http.Header),
		Host:   target.Host,
		Proto:  "HTTP/1.1",
	}

	// Copy WebSocket and auth headers
	for _, h := range []string{
		"Upgrade", "Connection",
		"Sec-WebSocket-Key", "Sec-WebSocket-Version",
		"Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions",
		"Authorization",
		"X-Session-ID", "X-User-Role",
		"Origin",
	} {
		if v := original.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}

	return req
}

// wsCopy copies data between connections with a timeout
func wsCopy(dst net.Conn, src net.Conn, timeout time.Duration) {
	buf := make([]byte, 32*1024)
	for {
		src.SetReadDeadline(time.Now().Add(timeout))
		n, err := src.Read(buf)
		if n > 0 {
			dst.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
