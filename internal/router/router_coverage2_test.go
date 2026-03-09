package router

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// --- singleJoiningSlash tests ---

func TestSingleJoiningSlash_BothSlash(t *testing.T) {
	result := singleJoiningSlash("http://api.com/", "/v1/chat")
	if result != "http://api.com/v1/chat" {
		t.Errorf("expected http://api.com/v1/chat, got %s", result)
	}
}

func TestSingleJoiningSlash_NeitherSlash(t *testing.T) {
	result := singleJoiningSlash("http://api.com", "v1/chat")
	if result != "http://api.com/v1/chat" {
		t.Errorf("expected http://api.com/v1/chat, got %s", result)
	}
}

func TestSingleJoiningSlash_ASlash(t *testing.T) {
	result := singleJoiningSlash("http://api.com/", "v1/chat")
	if result != "http://api.com/v1/chat" {
		t.Errorf("expected http://api.com/v1/chat, got %s", result)
	}
}

func TestSingleJoiningSlash_BSlash(t *testing.T) {
	result := singleJoiningSlash("http://api.com", "/v1/chat")
	if result != "http://api.com/v1/chat" {
		t.Errorf("expected http://api.com/v1/chat, got %s", result)
	}
}

func TestSingleJoiningSlash_Empty(t *testing.T) {
	result := singleJoiningSlash("", "")
	if result != "/" {
		t.Errorf("expected /, got %q", result)
	}
}

// --- fallbackRecorder tests ---

func TestFallbackRecorder_Write(t *testing.T) {
	fr := &fallbackRecorder{
		headers: make(http.Header),
	}

	n, err := fr.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if string(fr.body) != "hello" {
		t.Errorf("body should be hello, got %s", string(fr.body))
	}
	if fr.statusCode != http.StatusOK {
		t.Errorf("status should be 200 (implicit), got %d", fr.statusCode)
	}
}

func TestFallbackRecorder_WriteMultiple(t *testing.T) {
	fr := &fallbackRecorder{
		headers: make(http.Header),
	}

	fr.Write([]byte("hello "))
	fr.Write([]byte("world"))

	if string(fr.body) != "hello world" {
		t.Errorf("expected 'hello world', got %s", string(fr.body))
	}
}

func TestFallbackRecorder_WriteHeader(t *testing.T) {
	fr := &fallbackRecorder{
		headers: make(http.Header),
	}

	fr.WriteHeader(http.StatusBadGateway)
	if fr.statusCode != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", fr.statusCode)
	}
	if !fr.headerWritten {
		t.Error("headerWritten should be true")
	}
}

func TestFallbackRecorder_Header(t *testing.T) {
	fr := &fallbackRecorder{
		headers: make(http.Header),
	}

	fr.Header().Set("Content-Type", "application/json")
	if fr.Header().Get("Content-Type") != "application/json" {
		t.Error("header should be set")
	}
}

// --- AdaptFromProvider tests ---

func TestAdaptFromProvider_OpenAI(t *testing.T) {
	data := `{"id":"chatcmpl-1","choices":[{"message":{"content":"hello"}}],"model":"gpt-4"}`
	resp, err := AdaptFromProvider("openai", []byte(data))
	if err != nil {
		t.Fatalf("AdaptFromProvider error: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("expected content 'hello', got %s", resp.Content)
	}
}

func TestAdaptFromProvider_Unknown(t *testing.T) {
	// Unknown provider should still parse the JSON (falls back to raw parsing)
	resp, err := AdaptFromProvider("unknown-provider", []byte(`{"content":"hello"}`))
	// It may or may not error — just verify no panic
	_ = resp
	_ = err
}

func TestAdaptFromProvider_InvalidJSON(t *testing.T) {
	_, err := AdaptFromProvider("openai", []byte("not json"))
	if err == nil {
		t.Error("invalid JSON should return error")
	}
}

// --- Adapter edge cases ---

func TestAdaptToGemini_MinimalRequest(t *testing.T) {
	req := UnifiedRequest{
		Messages: []UnifiedMessage{
			{Role: "user", Content: "hello from gemini test"},
		},
	}
	data, err := adaptToGemini(req)
	if err != nil {
		t.Fatalf("adaptToGemini error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	contents, ok := result["contents"].([]any)
	if !ok || len(contents) == 0 {
		t.Error("should have contents array")
	}
}

func TestAdaptToOllama_MinimalRequest(t *testing.T) {
	req := UnifiedRequest{
		Model: "llama3",
		Messages: []UnifiedMessage{
			{Role: "user", Content: "hello from ollama"},
		},
	}
	data, err := adaptToOllama(req)
	if err != nil {
		t.Fatalf("adaptToOllama error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	if result["model"] != "llama3" {
		t.Errorf("model should be llama3, got %v", result["model"])
	}
}

func TestAdaptToAnthropic_WithSystemUnifiedMessage(t *testing.T) {
	req := UnifiedRequest{
		Messages: []UnifiedMessage{
			{Role: "system", Content: "you are helpful"},
			{Role: "user", Content: "hello"},
		},
	}
	data, err := adaptToAnthropic(req)
	if err != nil {
		t.Fatalf("adaptToAnthropic error: %v", err)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	if result["system"] != "you are helpful" {
		t.Errorf("system should be extracted, got %v", result["system"])
	}
}

// --- LoadConfig edge cases ---

func TestLoadConfig_EmptyProviders(t *testing.T) {
	content := `
providers: []
default_route: ""
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.yaml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := LoadConfig(path)
	// May succeed or fail depending on validation — we just check no panic
	_ = cfg
	_ = err
}

func TestLoadConfig_CodexRewrite(t *testing.T) {
	content := `
providers:
  - name: openai
    base_url: https://api.openai.com
    api_key: sk-test
    enabled: true
    timeout_sec: 30
codex_rewrite:
  enabled: true
  backend_url: https://chatgpt.com/backend-api/codex
default_route: openai
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "codex.yaml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if !cfg.CodexRewrite.Enabled {
		t.Error("codex_rewrite.enabled should be true")
	}
	if cfg.CodexRewrite.BackendURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("codex_rewrite.backend_url mismatch: %s", cfg.CodexRewrite.BackendURL)
	}
}
