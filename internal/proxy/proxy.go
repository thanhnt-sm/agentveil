package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/vurakit/agentveil/internal/auth"
	"github.com/vurakit/agentveil/internal/detector"
	"github.com/vurakit/agentveil/internal/promptguard"
	"github.com/vurakit/agentveil/internal/vault"
	"github.com/vurakit/agentveil/internal/webhook"
)

// Config holds proxy configuration
type Config struct {
	TargetURL   string // upstream LLM API base URL
	DefaultRole string // default role when X-User-Role not set (viewer/admin/operator)
}

// Option configures the Server
type Option func(*Server)

// WithAuth adds API key authentication
func WithAuth(am *auth.Manager) Option {
	return func(s *Server) { s.auth = am }
}

// WithPromptGuard adds prompt injection protection
func WithPromptGuard(pg *promptguard.Guard) Option {
	return func(s *Server) { s.promptGuard = pg }
}

// WithWebhook adds webhook notifications for PII events
func WithWebhook(d *webhook.Dispatcher) Option {
	return func(s *Server) { s.webhook = d }
}

// Server is the Agent Veil reverse proxy
type Server struct {
	config      Config
	proxy       *httputil.ReverseProxy
	target      *url.URL
	detector    *detector.Detector
	vault       *vault.Vault
	auth        *auth.Manager
	promptGuard *promptguard.Guard
	webhook     *webhook.Dispatcher
}

// New creates a new proxy Server
func New(cfg Config, det *detector.Detector, v *vault.Vault, opts ...Option) (*Server, error) {
	target, err := url.Parse(cfg.TargetURL)
	if err != nil {
		return nil, err
	}

	if cfg.DefaultRole == "" {
		cfg.DefaultRole = "viewer"
	}

	s := &Server{
		config:   cfg,
		target:   target,
		detector: det,
		vault:    v,
	}

	for _, opt := range opts {
		opt(s)
	}

	s.proxy = &httputil.ReverseProxy{
		Director:       s.director,
		ModifyResponse: s.modifyResponse,
		ErrorHandler:   s.errorHandler,
	}

	return s, nil
}

// MaxBodySize is the maximum allowed request body size (10MB)
const MaxBodySize = 10 * 1024 * 1024

// Handler returns the HTTP handler with middleware chain
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Chain: [auth →] [promptGuard →] securityEnforcer → RoleMiddleware → proxy
	var handler http.Handler = securityEnforcer(RoleMiddleware(s.config.DefaultRole)(s.proxy))
	if s.promptGuard != nil {
		handler = promptguard.Middleware(s.promptGuard)(handler)
	}
	if s.auth != nil {
		handler = s.auth.Middleware(handler)
	}
	mux.Handle("/v1/", handler)
	mux.Handle("/audit", http.HandlerFunc(s.handleAudit))
	mux.Handle("/scan", http.HandlerFunc(s.handleScan))
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/healthz", healthHandler)
	return mux
}

// director rewrites the request to the upstream target and anonymizes PII
func (s *Server) director(req *http.Request) {
	// Rewrite host/scheme to target
	req.URL.Scheme = s.target.Scheme
	req.URL.Host = s.target.Host
	req.Host = s.target.Host

	// Prepend target path if present (e.g., TARGET_URL=https://openrouter.ai/api)
	if s.target.Path != "" && s.target.Path != "/" {
		req.URL.Path = singleJoiningSlash(s.target.Path, req.URL.Path)
	}

	// Skip body processing for non-POST/PUT
	if req.Body == nil || (req.Method != http.MethodPost && req.Method != http.MethodPut) {
		return
	}

	sessionID := extractSessionID(req)
	anonymizeBody(req, s.detector, s.vault, sessionID, s.webhook)
}

// modifyResponse handles outbound rehydration for non-streaming responses
func (s *Server) modifyResponse(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")

	// For SSE streams, we handle rehydration in the streaming transport
	if strings.Contains(contentType, "text/event-stream") {
		sessionID := extractSessionIDFromResponse(resp)
		resp.Body = newSSERehydrator(resp.Body, s.vault, sessionID)
		return nil
	}

	// Standard JSON response - read, rehydrate, replace (bounded to 50MB)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	resp.Body.Close()
	if err != nil {
		return err
	}

	sessionID := extractSessionIDFromResponse(resp)
	role := resp.Request.Header.Get("X-User-Role")

	rehydrated := s.rehydrateText(string(body), sessionID, role)

	// P1 #6: Scan output for harmful content / data leaks
	if s.promptGuard != nil {
		result := s.promptGuard.ScanOutput(rehydrated)
		if len(result.Detections) > 0 {
			slog.Warn("output scan detected issues",
				"threat_level", result.ThreatLevel.String(),
				"detections", len(result.Detections),
				"session", sessionID)
		}
	}

	resp.Body = io.NopCloser(bytes.NewBufferString(rehydrated))
	resp.ContentLength = int64(len(rehydrated))

	return nil
}

// rehydrateText replaces pseudonym tokens with real values, applying role masking
func (s *Server) rehydrateText(text, sessionID, role string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	mappings, err := s.vault.LookupAll(ctx, sessionID)
	if err != nil || len(mappings) == 0 {
		return text
	}

	result := text
	for token, original := range mappings {
		replacement := original
		if strings.EqualFold(role, "viewer") {
			replacement = maskValue(original)
		}
		result = strings.ReplaceAll(result, token, replacement)
	}

	return result
}

// maskValue hides ~70% of a value for viewer role
func maskValue(val string) string {
	runes := []rune(val)
	n := len(runes)
	if n <= 3 {
		return val
	}

	visible := n * 30 / 100 // show ~30%
	if visible < 2 {
		visible = 2
	}
	front := visible / 2
	back := visible - front

	masked := make([]rune, n)
	for i := range masked {
		if i < front || i >= n-back {
			masked[i] = runes[i]
		} else {
			masked[i] = '*'
		}
	}
	return string(masked)
}

// errorHandler handles proxy errors
func (s *Server) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.Warn("upstream error", "error", err, "path", r.URL.Path)
	http.Error(w, `{"error":"upstream_error","message":"failed to reach LLM provider"}`, http.StatusBadGateway)
}

// extractSessionID gets session ID from request header or generates one
func extractSessionID(req *http.Request) string {
	sid := req.Header.Get("X-Session-ID")
	if sid == "" {
		sid = req.Header.Get("X-Request-ID")
	}
	if sid == "" {
		// IT3: Generate unique session ID to prevent cross-session PII leaks
		b := make([]byte, 8)
		rand.Read(b)
		sid = "anon_" + hex.EncodeToString(b)
	}
	return sid
}

func extractSessionIDFromResponse(resp *http.Response) string {
	if resp.Request != nil {
		return extractSessionID(resp.Request)
	}
	return "default"
}

// AnonymizeRequest returns a request modifier that anonymizes PII in the request body.
// Used by the router to apply PII protection in multi-provider mode.
func AnonymizeRequest(det *detector.Detector, v *vault.Vault, wh ...*webhook.Dispatcher) func(*http.Request) {
	var dispatcher *webhook.Dispatcher
	if len(wh) > 0 {
		dispatcher = wh[0]
	}

	return func(req *http.Request) {
		if req.Body == nil || (req.Method != http.MethodPost && req.Method != http.MethodPut) {
			return
		}
		sessionID := extractSessionID(req)
		anonymizeBody(req, det, v, sessionID, dispatcher)
	}
}

// RehydrateResponse returns a response modifier that rehydrates PII tokens in responses.
// Used by the router to apply PII rehydration in multi-provider mode.
func RehydrateResponse(v *vault.Vault, defaultRole string) func(*http.Response) error {
	return func(resp *http.Response) error {
		contentType := resp.Header.Get("Content-Type")

		sessionID := extractSessionIDFromResponse(resp)
		role := ""
		if resp.Request != nil {
			role = resp.Request.Header.Get("X-User-Role")
		}
		if role == "" {
			role = defaultRole
		}

		// For SSE streams, wrap with streaming rehydrator
		if strings.Contains(contentType, "text/event-stream") {
			resp.Body = newSSERehydrator(resp.Body, v, sessionID)
			return nil
		}

		// Standard JSON response — read, rehydrate, replace (bounded to 50MB)
		body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		mappings, err := v.LookupAll(ctx, sessionID)
		if err != nil || len(mappings) == 0 {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}

		result := string(body)
		for token, original := range mappings {
			replacement := original
			if strings.EqualFold(role, "viewer") {
				replacement = maskValue(original)
			}
			result = strings.ReplaceAll(result, token, replacement)
		}

		slog.Info("PII rehydrated", "count", len(mappings), "session", sessionID, "role", role)

		resp.Body = io.NopCloser(bytes.NewBufferString(result))
		resp.ContentLength = int64(len(result))
		return nil
	}
}

// singleJoiningSlash joins two URL path segments with exactly one slash.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
