package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// === ShouldBypass Detection Tests ===

func TestBypass_CodexJWT_OnResponsesPath(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		BackendURL: "https://chatgpt.com/backend-api/codex",
		UserAgents: []string{"codex-cli/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulated Codex OAuth JWT (3-part dot-separated)
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJjbGllbnRfaWQiOiJhcHBfRU1vYW1FRVo3M2YwQ2tYYVhwN2hyYW5uIiwic2NwIjpbIm1vZGVsLnJlYWQiXX0.fakesig"

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("Authorization", "Bearer "+jwt)

	shouldBypass, reason := bp.ShouldBypass(req)
	if !shouldBypass {
		t.Error("JWT on /v1/responses should trigger bypass")
	}
	if reason != "jwt-bearer" {
		t.Errorf("expected reason 'jwt-bearer', got %q", reason)
	}
}

func TestBypass_JWT_OnChatCompletions(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// JWT on /v1/chat/completions — should ALSO bypass (broadened detection)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Authorization", "Bearer eyJhbGci.eyJ0ZXN0Ig.sig")

	shouldBypass, reason := bp.ShouldBypass(req)
	if !shouldBypass {
		t.Error("JWT on any path should trigger bypass")
	}
	if reason != "jwt-bearer" {
		t.Errorf("expected 'jwt-bearer', got %q", reason)
	}
}

func TestBypass_APIKey_DoesNotBypass(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		BackendURL: "https://chatgpt.com/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("Authorization", "Bearer sk-proj-abc123xyz")

	shouldBypass, _ := bp.ShouldBypass(req)
	if shouldBypass {
		t.Error("sk- API key should NOT trigger bypass — should go through normal pipeline")
	}
}

func TestBypass_VeilKey_DoesNotBypass(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		BackendURL: "https://chatgpt.com/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("Authorization", "Bearer veil_sk_abc123")

	shouldBypass, _ := bp.ShouldBypass(req)
	if shouldBypass {
		t.Error("veil_sk_ key should NOT trigger bypass")
	}
}

func TestBypass_UserAgent_CodexCLI(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		BackendURL: "https://chatgpt.com/backend-api/codex",
		UserAgents: []string{"codex-cli/", "claude-cli/", "antigravity/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("User-Agent", "codex-cli/1.0.5")
	req.Header.Set("Authorization", "Bearer sk-test")

	shouldBypass, reason := bp.ShouldBypass(req)
	if !shouldBypass {
		t.Error("codex-cli User-Agent should trigger bypass")
	}
	if !strings.Contains(reason, "user-agent") {
		t.Errorf("expected user-agent reason, got %q", reason)
	}
}

func TestBypass_UserAgent_ClaudeCLI_NoAuth(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		UserAgents: []string{"claude-cli/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Claude CLI user-agent match → bypass should trigger
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("User-Agent", "claude-cli/2.0")

	shouldBypass, reason := bp.ShouldBypass(req)
	if !shouldBypass {
		t.Error("claude-cli User-Agent should trigger bypass")
	}
	if !strings.Contains(reason, "claude-cli/") {
		t.Errorf("expected 'claude-cli/' in reason, got %q", reason)
	}
}

func TestBypass_UserAgent_ClaudeCLI_Triggers(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		UserAgents: []string{"claude-cli/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("User-Agent", "claude-cli/2.0")
	req.Header.Set("Authorization", "Bearer some-token")

	shouldBypass, reason := bp.ShouldBypass(req)
	if !shouldBypass {
		t.Error("claude-cli User-Agent should trigger bypass")
	}
	if !strings.Contains(reason, "claude-cli/") {
		t.Errorf("expected 'claude-cli/' in reason, got %q", reason)
	}
}

func TestBypass_UnknownUserAgent_DoesNotBypass(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		UserAgents: []string{"codex-cli/", "claude-cli/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"prompt":"test"}`))
	req.Header.Set("User-Agent", "python-requests/2.31")
	req.Header.Set("Authorization", "Bearer sk-test-key")

	shouldBypass, _ := bp.ShouldBypass(req)
	if shouldBypass {
		t.Error("unknown User-Agent should NOT trigger bypass")
	}
}

func TestBypass_NoAuthHeader_DoesNotBypass(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		UserAgents: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/responses", nil)
	// No auth header, no user-agent

	shouldBypass, _ := bp.ShouldBypass(req)
	if shouldBypass {
		t.Error("request without auth or user-agent should NOT trigger bypass")
	}
}

func TestBypass_UserAgent_CaseInsensitive(t *testing.T) {
	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		UserAgents: []string{"Antigravity/"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("User-Agent", "antigravity/3.0 (macOS)")

	shouldBypass, _ := bp.ShouldBypass(req)
	if !shouldBypass {
		t.Error("case-insensitive User-Agent match should trigger bypass")
	}
}

// === Codex Passthrough E2E Test ===

func TestBypass_ServeCodex_ForwardsToBackend(t *testing.T) {
	// Mock ChatGPT backend
	var receivedPath string
	var receivedAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		BackendURL: backend.URL + "/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}

	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJ0ZXN0IjoicGF5bG9hZCJ9.fakesig"
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+jwt)

	w := httptest.NewRecorder()
	bp.ServeCodex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(receivedPath, "/responses") {
		t.Errorf("expected path to contain '/responses', got %s", receivedPath)
	}
	if receivedAuth != "Bearer "+jwt {
		t.Error("Authorization header should pass through untouched")
	}
}

func TestBypass_ServeCodex_SubPath(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	bp, err := newIDEBypass(BypassConfig{
		Enabled:    true,
		BackendURL: backend.URL + "/backend-api/codex",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/responses/resp_123/cancel", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGci.eyJ0ZXN0Ig.sig")

	w := httptest.NewRecorder()
	bp.ServeCodex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.HasSuffix(receivedPath, "/responses/resp_123/cancel") {
		t.Errorf("expected subpath preserved, got %s", receivedPath)
	}
}

// === IsCodexPath Tests ===

func TestIsCodexPath(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"/v1/responses", true},
		{"/v1/responses/resp_123", true},
		{"/v1/responses/resp_123/cancel", true},
		{"/v1/chat/completions", false},
		{"/v1/models", false},
		{"/health", false},
	}

	for _, tt := range tests {
		if got := IsCodexPath(tt.path); got != tt.expect {
			t.Errorf("IsCodexPath(%q) = %v, want %v", tt.path, got, tt.expect)
		}
	}
}

// === Integration: Bypass in Router ===

func TestRouter_BypassEnabled_CodexJWT_SkipsAuth(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"resp_123"}`))
	}))
	defer backend.Close()

	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: backend.URL, AuthMethod: "passthrough", Enabled: true, Weight: 1, MaxRetries: 2, TimeoutSec: 30},
		},
		Routes: []RouteConfig{
			{PathPrefix: "/v1/responses", Provider: "openai"},
		},
		DefaultRoute: "openai",
		Bypass: BypassConfig{
			Enabled:    true,
			BackendURL: backend.URL + "/backend-api/codex",
		},
	}

	rt, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJ0ZXN0IjoicGF5bG9hZCJ9.fakesig"
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+jwt)

	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	// Should have been routed to Codex backend, not the provider
	if !strings.Contains(receivedPath, "/responses") {
		t.Errorf("expected path to contain '/responses', got %s", receivedPath)
	}
}

func TestRouter_BypassEnabled_JWT_OnChat_SkipsPII(t *testing.T) {
	var receivedPath string
	var receivedBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer backend.Close()

	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: backend.URL, AuthMethod: "passthrough", Enabled: true, Weight: 1, MaxRetries: 2, TimeoutSec: 30},
		},
		Routes: []RouteConfig{
			{PathPrefix: "/v1/chat", Provider: "openai"},
		},
		DefaultRoute: "openai",
		Bypass: BypassConfig{
			Enabled:    true,
			BackendURL: backend.URL + "/backend-api/codex",
		},
	}

	rt, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// JWT on /v1/chat/completions → should bypass, PII should NOT be anonymized
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJ0ZXN0IjoicGF5bG9hZCJ9.fakesig"
	body := `{"messages":[{"role":"user","content":"My email is test@example.com"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+jwt)

	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	// Verify body was NOT anonymized (email should pass through as-is)
	if !strings.Contains(receivedBody, "test@example.com") {
		t.Errorf("body should NOT be anonymized for bypass traffic, got: %s", receivedBody)
	}
	if !strings.Contains(receivedPath, "/chat/completions") {
		t.Errorf("expected path /v1/chat/completions, got %s", receivedPath)
	}
}

func TestRouter_BypassDisabled_FallsBackToCodexRewrite(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: backend.URL, AuthMethod: "passthrough", Enabled: true, Weight: 1, MaxRetries: 2, TimeoutSec: 30},
		},
		Routes: []RouteConfig{
			{PathPrefix: "/v1/responses", Provider: "openai"},
		},
		DefaultRoute: "openai",
		CodexRewrite: CodexRewriteConfig{
			Enabled:    true,
			BackendURL: backend.URL + "/backend-api/codex",
		},
	}

	rt, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Codex OAuth JWT with matching client_id
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJjbGllbnRfaWQiOiJhcHBfRU1vYW1FRVo3M2YwQ2tYYVhwN2hyYW5uIiwic2NwIjpbIm1vZGVsLnJlYWQiXX0.fakesig"
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+jwt)

	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(receivedPath, "/backend-api/codex/responses") {
		t.Errorf("expected legacy codex rewrite path, got %s", receivedPath)
	}
}

func TestRouter_BypassEnabled_APIKey_GoesNormalPipeline(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := &RouterConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: backend.URL, AuthMethod: "passthrough", Enabled: true, Weight: 1, MaxRetries: 2, TimeoutSec: 30},
		},
		Routes: []RouteConfig{
			{PathPrefix: "/v1/responses", Provider: "openai"},
		},
		DefaultRoute: "openai",
		Bypass: BypassConfig{
			Enabled:    true,
			BackendURL: backend.URL + "/backend-api/codex",
		},
	}

	rt, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Regular API key — should NOT bypass
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	req.Header.Set("Authorization", "Bearer sk-proj-test-key-12345")

	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	// Should go through normal pipeline to provider, NOT to codex backend
	if strings.Contains(receivedPath, "/backend-api/codex") {
		t.Error("API key traffic should NOT be routed to Codex backend")
	}
}
