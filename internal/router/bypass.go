package router

import (
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// BypassConfig configures the IDE/CLI passthrough bypass.
// When enabled, connections from known IDE/CLI tools (Codex IDE, Codex CLI,
// Claude CLI, Antigravity IDE) are forwarded directly to their target servers
// without any auth, PII scanning, prompt guard, or rate limiting.
type BypassConfig struct {
	Enabled    bool     `yaml:"enabled"`
	BackendURL string   `yaml:"backend_url"`  // Codex backend (default: chatgpt.com/backend-api/codex)
	UserAgents []string `yaml:"user_agents"`  // User-Agent substrings that trigger bypass
}

// ideBypass handles passthrough for IDE/CLI connections.
type ideBypass struct {
	// codexProxy forwards Codex OAuth traffic to ChatGPT backend.
	codexProxy *httputil.ReverseProxy
	codexURL   *url.URL

	// userAgents is the list of User-Agent substrings that trigger bypass.
	userAgents []string

	// logger for bypass events.
	logger *slog.Logger
}

// newIDEBypass creates the bypass handler for IDE/CLI connections.
func newIDEBypass(cfg BypassConfig) (*ideBypass, error) {
	backendURL := cfg.BackendURL
	if backendURL == "" {
		backendURL = DefaultCodexBackendURL
	}

	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, err
	}

	bp := &ideBypass{
		codexURL:   target,
		userAgents: cfg.UserAgents,
		logger:     slog.Default(),
	}

	bp.codexProxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Rewrite: /v1/responses{/subpath} → /backend-api/codex/responses{/subpath}
			subpath := strings.TrimPrefix(req.URL.Path, "/v1/responses")
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = target.Path + "/responses" + subpath

			bp.logger.Info("bypass: codex passthrough",
				"original", "/v1/responses"+subpath,
				"target", req.URL.String(),
			)
			// Authorization header passes through untouched
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			bp.logger.Error("bypass: codex upstream error", "error", err)
			http.Error(w, `{"error":"codex_backend_error","message":"failed to reach Codex backend"}`, http.StatusBadGateway)
		},
		Transport: &http.Transport{
			ResponseHeaderTimeout: 120 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        20,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	return bp, nil
}

// BypassContextKey is used to signal bypass mode to downstream handlers.
// When set on request context, PII anonymization and rehydration are skipped.
const BypassContextKey = "x-agentveil-bypass"

// ShouldBypass checks if the request should bypass all AgentVeil processing.
// Returns true + a reason string if bypass is triggered.
//
// Detection priority:
//  1. JWT Bearer token → bypass (Codex OAuth, any IDE using JWT auth)
//  2. Known User-Agent patterns → bypass (codex-cli, claude-cli, etc.)
//  3. Everything else → normal pipeline
func (bp *ideBypass) ShouldBypass(req *http.Request) (bool, string) {
	authHeader := req.Header.Get("Authorization")
	userAgent := req.Header.Get("User-Agent")

	// Check 1: Any JWT Bearer token → bypass
	// API keys (sk-*, veil_sk_*) are NOT JWTs and go through normal pipeline.
	// JWTs are used by IDE/CLI tools (Codex IDE, Codex CLI via OAuth login).
	if authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		if !strings.HasPrefix(token, "sk-") && !strings.HasPrefix(token, "veil_sk_") {
			parts := strings.SplitN(token, ".", 3)
			if len(parts) == 3 {
				return true, "jwt-bearer"
			}
		}
	}

	// Check 2: Known IDE/CLI User-Agent patterns
	if userAgent != "" {
		uaLower := strings.ToLower(userAgent)
		for _, pattern := range bp.userAgents {
			if strings.Contains(uaLower, strings.ToLower(pattern)) {
				return true, "user-agent:" + pattern
			}
		}
	}

	return false, ""
}

// ServeCodex forwards Codex traffic directly to the ChatGPT backend.
func (bp *ideBypass) ServeCodex(w http.ResponseWriter, r *http.Request) {
	bp.codexProxy.ServeHTTP(w, r)
}

// ServeProvider forwards bypassed non-Codex traffic to a provider without PII processing.
// Creates a clean reverse proxy that only does URL rewriting and auth passthrough.
func (bp *ideBypass) ServeProvider(w http.ResponseWriter, r *http.Request, target *url.URL, authMethod, apiKey, authParam string) {
	cleanProxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			if target.Path != "" && target.Path != "/" {
				req.URL.Path = target.Path + req.URL.Path
			}

			// Apply provider auth only if not passthrough
			if authMethod != "passthrough" && apiKey != "" {
				switch authMethod {
				case "query":
					q := req.URL.Query()
					param := authParam
					if param == "" {
						param = "key"
					}
					q.Set(param, apiKey)
					req.URL.RawQuery = q.Encode()
				case "x-api-key":
					req.Header.Set("x-api-key", apiKey)
				default:
					req.Header.Set("Authorization", "Bearer "+apiKey)
				}
			}
			// No PII anonymization, no request modifiers
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			bp.logger.Error("bypass: provider upstream error", "error", err, "target", target.Host)
			http.Error(w, `{"error":"provider_error","message":"failed to reach provider"}`, http.StatusBadGateway)
		},
	}
	cleanProxy.ServeHTTP(w, r)
}

// IsCodexPath returns true if the request path is a Codex API path.
func IsCodexPath(path string) bool {
	return strings.HasPrefix(path, "/v1/responses")
}
