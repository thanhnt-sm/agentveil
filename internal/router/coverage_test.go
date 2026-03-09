package router

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// --- IsCodexOAuthToken tests ---

func makeJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadBytes, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(payloadBytes)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return fmt.Sprintf("%s.%s.%s", header, body, sig)
}

func TestIsCodexOAuthToken_APIKey(t *testing.T) {
	if IsCodexOAuthToken("Bearer sk-test-key-123") {
		t.Error("sk- API key should not be detected as Codex OAuth")
	}
}

func TestIsCodexOAuthToken_VeilKey(t *testing.T) {
	if IsCodexOAuthToken("Bearer veil_sk_abc123") {
		t.Error("veil_sk_ key should not be detected as Codex OAuth")
	}
}

func TestIsCodexOAuthToken_NotJWT(t *testing.T) {
	if IsCodexOAuthToken("Bearer not-a-jwt") {
		t.Error("non-JWT token should not be detected as Codex OAuth")
	}
}

func TestIsCodexOAuthToken_InvalidBase64(t *testing.T) {
	if IsCodexOAuthToken("Bearer aaa.!!!invalid!!!.ccc") {
		t.Error("invalid base64 should not be detected as Codex OAuth")
	}
}

func TestIsCodexOAuthToken_InvalidJSON(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`not json`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	token := fmt.Sprintf("Bearer %s.%s.%s", header, body, sig)
	if IsCodexOAuthToken(token) {
		t.Error("invalid JSON payload should not be Codex OAuth")
	}
}

func TestIsCodexOAuthToken_CodexClientID(t *testing.T) {
	token := makeJWT(map[string]any{
		"client_id": CodexOAuthClientID,
		"exp":       1999999999,
	})
	if !IsCodexOAuthToken("Bearer " + token) {
		t.Error("JWT with Codex client_id should be detected as Codex OAuth")
	}
}

func TestIsCodexOAuthToken_NonCodexClientID(t *testing.T) {
	token := makeJWT(map[string]any{
		"client_id": "other-client-id",
		"exp":       1999999999,
	})
	if IsCodexOAuthToken("Bearer " + token) {
		t.Error("JWT with non-Codex client_id and no scopes should not be Codex OAuth")
	}
}

func TestIsCodexOAuthToken_ScopesWithoutResponsesWrite(t *testing.T) {
	token := makeJWT(map[string]any{
		"scp": []any{"model.read", "model.request"},
		"exp": 1999999999,
	})
	if !IsCodexOAuthToken("Bearer " + token) {
		t.Error("JWT with scopes but missing api.responses.write should be Codex OAuth")
	}
}

func TestIsCodexOAuthToken_ScopesWithResponsesWrite(t *testing.T) {
	token := makeJWT(map[string]any{
		"scp": []any{"model.read", "api.responses.write"},
		"exp": 1999999999,
	})
	if IsCodexOAuthToken("Bearer " + token) {
		t.Error("JWT with api.responses.write scope should NOT be Codex OAuth")
	}
}

func TestIsCodexOAuthToken_EmptyString(t *testing.T) {
	if IsCodexOAuthToken("") {
		t.Error("empty string should not be Codex OAuth")
	}
}

// --- LoadConfig tests ---

func TestLoadConfig_ValidYAML(t *testing.T) {
	content := `
providers:
  - name: openai
    base_url: https://api.openai.com
    api_key: sk-test
    enabled: true
    timeout_sec: 30
routes:
  - path_prefix: /v1
    provider: openai
default_route: openai
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "router.yaml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "openai" {
		t.Errorf("expected provider name 'openai', got %s", cfg.Providers[0].Name)
	}
	if cfg.DefaultRoute != "openai" {
		t.Errorf("expected default_route 'openai', got %s", cfg.DefaultRoute)
	}
}

func TestLoadConfig_InvalidPath(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/router.yaml")
	if err == nil {
		t.Error("should return error for nonexistent file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.yaml")
	// Write content that parses as YAML but produces an invalid config (no providers)
	os.WriteFile(path, []byte(`providers: "not-a-list"`), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("should return error for invalid config structure")
	}
}

func TestLoadConfig_MultipleProviders(t *testing.T) {
	content := `
providers:
  - name: openai
    base_url: https://api.openai.com
    api_key: sk-1
    enabled: true
    timeout_sec: 30
  - name: anthropic
    base_url: https://api.anthropic.com
    api_key: sk-2
    enabled: true
    timeout_sec: 30
    auth_method: x-api-key
  - name: disabled
    base_url: https://disabled.com
    enabled: false
default_route: openai
load_balance: round-robin
fallback:
  enabled: true
  max_attempts: 3
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "multi.yaml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if len(cfg.Providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(cfg.Providers))
	}
	if cfg.LoadBalance != "round-robin" {
		t.Errorf("expected load_balance 'round-robin', got %s", cfg.LoadBalance)
	}
	if !cfg.Fallback.Enabled {
		t.Error("expected fallback.enabled=true")
	}
}

// --- dialUpstream tests ---

// dialUpstream tests are omitted because they require real network I/O
// which would slow down CI and produce flaky results.


// --- buildProvider tests ---

func TestBuildProvider_InvalidURL(t *testing.T) {
	r := &Router{
		providers: make(map[string]*Provider),
	}
	_, err := r.buildProvider(ProviderConfig{
		Name:    "bad",
		BaseURL: "://invalid-url",
	})
	if err == nil {
		t.Error("should return error for invalid URL")
	}
}

func TestBuildProvider_ValidProvider(t *testing.T) {
	r := &Router{
		providers: make(map[string]*Provider),
	}
	p, err := r.buildProvider(ProviderConfig{
		Name:       "test",
		BaseURL:    "https://api.example.com",
		APIKey:     "test-key",
		TimeoutSec: 30,
	})
	if err != nil {
		t.Fatalf("buildProvider error: %v", err)
	}
	if p.Config.Name != "test" {
		t.Error("provider Name should be 'test'")
	}
	if !p.healthy.Load() {
		t.Error("new provider should be healthy")
	}
	if p.Proxy == nil {
		t.Error("proxy should be set")
	}
}

// --- newCodexRewriter tests ---

func TestNewCodexRewriter_Valid(t *testing.T) {
	cr, err := newCodexRewriter("https://chatgpt.com/backend-api/codex")
	if err != nil {
		t.Fatalf("newCodexRewriter error: %v", err)
	}
	if cr == nil {
		t.Fatal("should return non-nil rewriter")
	}
	if cr.backendURL.Host != "chatgpt.com" {
		t.Errorf("expected host chatgpt.com, got %s", cr.backendURL.Host)
	}
}

func TestNewCodexRewriter_InvalidURL(t *testing.T) {
	_, err := newCodexRewriter("://invalid")
	if err == nil {
		t.Error("should return error for invalid URL")
	}
}
