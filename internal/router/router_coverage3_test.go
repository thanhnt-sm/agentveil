package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- New() config combos ---

func TestNew_DisabledProvider(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer backend.Close()

	cfgContent := `
providers:
  - name: active
    base_url: ` + backend.URL + `
    api_key: sk-active
    enabled: true
    timeout_sec: 5
  - name: disabled
    base_url: http://localhost:1
    api_key: sk-disabled
    enabled: false
    timeout_sec: 5
default_route: active
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Should only have 1 provider
	if len(r.providers) != 1 {
		t.Errorf("expected 1 provider (disabled skipped), got %d", len(r.providers))
	}
	if _, ok := r.providers["disabled"]; ok {
		t.Error("disabled provider should be skipped")
	}
}

func TestNew_NoEnabledProviders(t *testing.T) {
	cfgContent := `
providers:
  - name: p1
    base_url: http://localhost:1
    api_key: sk-1
    enabled: false
default_route: p1
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	_, err := New(cfg)
	if err == nil {
		t.Error("should error with no enabled providers")
	}
	if !strings.Contains(err.Error(), "no enabled") {
		t.Errorf("expected 'no enabled' error, got: %v", err)
	}
}

func TestNew_AutoDefaultRoute(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer backend.Close()

	cfgContent := `
providers:
  - name: auto-pick
    base_url: ` + backend.URL + `
    api_key: sk-auto
    enabled: true
    timeout_sec: 5
default_route: ""
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if r.defaultRoute == "" {
		t.Error("default route should be auto-picked")
	}
}

func TestNew_WithRoutes(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
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
  - path_prefix: /v1/chat
    provider: openai
  - path_prefix: /v1
    provider: openai
default_route: openai
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Routes should be sorted by length descending
	if len(r.sortedRoutes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(r.sortedRoutes))
	}
	if len(r.sortedRoutes) >= 2 && r.sortedRoutes[0] != "/v1/chat" {
		t.Error("longer prefix should be first")
	}
}

func TestNew_WithCodexRewrite(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer backend.Close()

	cfgContent := `
providers:
  - name: openai
    base_url: ` + backend.URL + `
    api_key: sk-test
    enabled: true
    timeout_sec: 5
codex_rewrite:
  enabled: true
  backend_url: https://chatgpt.com/backend-api/codex
default_route: openai
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if r.codexRewriter == nil {
		t.Error("codex rewriter should be initialized")
	}
}

func TestNew_WeightedLoadBalance(t *testing.T) {
	b1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer b1.Close()
	b2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer b2.Close()

	cfgContent := `
providers:
  - name: p1
    base_url: ` + b1.URL + `
    api_key: sk-1
    enabled: true
    timeout_sec: 5
    weight: 3
  - name: p2
    base_url: ` + b2.URL + `
    api_key: sk-2
    enabled: true
    timeout_sec: 5
    weight: 1
load_balance: weighted
default_route: p1
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if r.strategy != "weighted" {
		t.Errorf("expected weighted, got %s", r.strategy)
	}
}

// --- newCodexRewriter ---

func TestNewCodexRewriter_ValidURL(t *testing.T) {
	cr, err := newCodexRewriter("https://chatgpt.com/backend-api/codex")
	if err != nil {
		t.Fatalf("newCodexRewriter error: %v", err)
	}
	if cr == nil {
		t.Error("should return a codex rewriter")
	}
	if cr.proxy == nil {
		t.Error("proxy should be initialized")
	}
}

func TestNewCodexRewriter_InvalidScheme(t *testing.T) {
	_, err := newCodexRewriter("://invalid")
	if err == nil {
		t.Error("should error on invalid URL")
	}
}

// --- Adapter edge cases ---

func TestAdaptToGemini_SystemMessage(t *testing.T) {
	req := UnifiedRequest{
		Messages: []UnifiedMessage{
			{Role: "system", Content: "you are a helper"},
			{Role: "user", Content: "hello"},
		},
	}
	data, err := adaptToGemini(req)
	if err != nil {
		t.Fatalf("adaptToGemini error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	// Gemini should handle system message
	if result == nil {
		t.Error("should produce valid output")
	}
}

func TestAdaptToAnthropic_NoSystem(t *testing.T) {
	req := UnifiedRequest{
		Messages: []UnifiedMessage{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 100,
	}
	data, err := adaptToAnthropic(req)
	if err != nil {
		t.Fatalf("adaptToAnthropic error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	if result["system"] != nil && result["system"] != "" {
		t.Error("should not have system when none provided")
	}
}

func TestAdaptToOllama_WithOptions(t *testing.T) {
	req := UnifiedRequest{
		Model: "llama3",
		Messages: []UnifiedMessage{
			{Role: "user", Content: "hello"},
		},
		Temperature: 0.7,
		MaxTokens:   200,
		Stream:      true,
	}
	data, err := adaptToOllama(req)
	if err != nil {
		t.Fatalf("adaptToOllama error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	if result["stream"] != true {
		t.Error("stream should be true")
	}
}

// --- buildProvider edge cases ---

func TestBuildProvider_WithWeight(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer backend.Close()

	cfgContent := `
providers:
  - name: weighted-p
    base_url: ` + backend.URL + `
    api_key: sk-w
    enabled: true
    timeout_sec: 10
    weight: 5
default_route: weighted-p
`
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "cfg.yaml")
	os.WriteFile(p, []byte(cfgContent), 0644)
	cfg, _ := LoadConfig(p)

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	provider := r.providers["weighted-p"]
	if provider == nil {
		t.Fatal("provider should exist")
	}
}
