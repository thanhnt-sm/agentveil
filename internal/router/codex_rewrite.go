package router

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	// CodexOAuthClientID is the OAuth client_id used by Codex CLI when logged in via ChatGPT.
	CodexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// DefaultCodexBackendURL is the ChatGPT backend endpoint for Codex.
	// See: https://github.com/openai/codex — codex-rs/core/src/model_provider_info.rs
	DefaultCodexBackendURL = "https://chatgpt.com/backend-api/codex"
)

// CodexRewriteConfig configures the Codex OAuth URL rewrite behavior.
type CodexRewriteConfig struct {
	Enabled    bool   `yaml:"enabled"`
	BackendURL string `yaml:"backend_url"` // default: https://chatgpt.com/backend-api/codex
}

// IsCodexOAuthToken checks if the Authorization header contains a Codex OAuth JWT.
// It decodes the JWT payload (without verifying the signature) and checks:
//  1. client_id == CodexOAuthClientID
//  2. OR scopes ("scp") do NOT contain "api.responses.write"
//
// Returns false for API keys (sk-*, veil_sk_*) and non-JWT tokens.
func IsCodexOAuthToken(authHeader string) bool {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)

	// API keys are not JWTs
	if strings.HasPrefix(token, "sk-") || strings.HasPrefix(token, "veil_sk_") {
		return false
	}

	// JWTs have 3 parts separated by dots
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return false
	}

	// Decode payload (part[1]) — add padding for base64url
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}

	// Check 1: Codex OAuth client_id
	if clientID, ok := claims["client_id"].(string); ok {
		if clientID == CodexOAuthClientID {
			return true
		}
	}

	// Check 2: Has scopes but missing api.responses.write
	if scopes, ok := claims["scp"].([]any); ok && len(scopes) > 0 {
		for _, s := range scopes {
			if str, ok := s.(string); ok && str == "api.responses.write" {
				return false // Has the scope → regular API token, not Codex OAuth
			}
		}
		return true // Has scopes but missing api.responses.write → Codex OAuth
	}

	return false
}

// codexRewriter handles URL rewriting for Codex OAuth requests.
type codexRewriter struct {
	backendURL *url.URL
	proxy      *httputil.ReverseProxy
}

// newCodexRewriter creates a reverse proxy targeting the ChatGPT backend.
func newCodexRewriter(backendURL string) (*codexRewriter, error) {
	target, err := url.Parse(backendURL)
	if err != nil {
		return nil, fmt.Errorf("codex rewrite: invalid backend URL %q: %w", backendURL, err)
	}

	cr := &codexRewriter{backendURL: target}

	cr.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Rewrite: /v1/responses{/subpath} → /backend-api/codex/responses{/subpath}
			subpath := strings.TrimPrefix(req.URL.Path, "/v1/responses")
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = target.Path + "/responses" + subpath

			slog.Info("codex rewrite",
				"original", "/v1/responses"+subpath,
				"target", req.URL.String(),
			)

			// Authorization header passes through untouched (already set by client)
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			slog.Error("codex rewrite: upstream error", "error", err)
			http.Error(w, `{"error":"codex_backend_error","message":"failed to reach Codex backend"}`, http.StatusBadGateway)
		},
		Transport: &http.Transport{
			ResponseHeaderTimeout: 120 * time.Second, // Codex responses can be slow
		},
	}

	return cr, nil
}

// ServeHTTP forwards the request to the ChatGPT backend.
func (cr *codexRewriter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cr.proxy.ServeHTTP(w, r)
}
