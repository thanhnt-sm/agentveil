package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupRouterWithBackend creates a Router with a single httptest.Server as backend provider
func setupRouterWithBackend(t *testing.T, handler http.HandlerFunc) (*Router, *httptest.Server) {
	t.Helper()
	backend := httptest.NewServer(handler)
	t.Cleanup(backend.Close)

	cfgContent := `
providers:
  - name: test-provider
    base_url: ` + backend.URL + `
    api_key: sk-test-key
    enabled: true
    timeout_sec: 5
default_route: test-provider
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "router.yaml")
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	return r, backend
}

func TestRouter_ServeHTTP_BasicProxy(t *testing.T) {
	r, _ := setupRouterWithBackend(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"content":"hello"}}]}`))
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "chatcmpl") {
		t.Errorf("expected proxied response, got: %s", body)
	}
}

func TestRouter_ServeHTTP_GETModels(t *testing.T) {
	r, _ := setupRouterWithBackend(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-4"}]}`))
	})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRouter_ServeHTTP_StreamResponse(t *testing.T) {
	r, _ := setupRouterWithBackend(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("data: {\"content\":\"hello\"}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRouter_ServeHTTP_BackendError(t *testing.T) {
	r, _ := setupRouterWithBackend(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal server error"}`))
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Should proxy back the 500
	if w.Code != 500 {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestRouter_ServeHTTP_HeadersForwarded(t *testing.T) {
	var receivedAuth string
	r, _ := setupRouterWithBackend(t, func(w http.ResponseWriter, req *http.Request) {
		receivedAuth = req.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Provider API key should be set by the proxy
	if receivedAuth == "" {
		t.Error("Authorization header should be forwarded to backend")
	}
}

func TestRouter_ServeHTTP_BodyPreserved(t *testing.T) {
	var receivedBody string
	r, _ := setupRouterWithBackend(t, func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		receivedBody = string(b)
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	})

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if receivedBody == "" {
		t.Error("body should be forwarded to backend")
	}
}

func TestRouter_ServeHTTP_PathStripRoutePrefix(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		receivedPath = req.URL.Path
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	cfgContent := `
providers:
  - name: openai
    base_url: ` + backend.URL + `
    api_key: sk-test
    enabled: true
    timeout_sec: 5
routes:
  - path_prefix: /v1
    provider: openai
default_route: openai
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "route.yaml")
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	cfg, _ := LoadConfig(cfgPath)
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Path should still contain the expected endpoint
	if receivedPath == "" {
		t.Error("path should be forwarded")
	}
}

// --- Fallback integration test ---

func TestRouter_FallbackToSecondProvider(t *testing.T) {
	// Primary backend returns 500
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"primary down"}`))
	}))
	defer primary.Close()

	// Secondary backend returns 200
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[{"message":{"content":"from secondary"}}]}`))
	}))
	defer secondary.Close()

	cfgContent := `
providers:
  - name: primary
    base_url: ` + primary.URL + `
    api_key: sk-1
    enabled: true
    timeout_sec: 5
  - name: secondary
    base_url: ` + secondary.URL + `
    api_key: sk-2
    enabled: true
    timeout_sec: 5
default_route: primary
fallback:
  enabled: true
  max_attempts: 2
  retry_delay_sec: 0
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "fallback.yaml")
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	cfg, _ := LoadConfig(cfgPath)
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Should fall back to secondary
	if w.Code != 200 {
		t.Errorf("expected 200 from fallback, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "from secondary") {
		t.Error("response should come from secondary provider")
	}
}
