package agentveil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTransport_Defaults(t *testing.T) {
	tr := NewTransport(Config{ProxyURL: "http://localhost:8080"}, nil)
	if tr == nil {
		t.Fatal("NewTransport returned nil")
	}
	// SessionID should be auto-generated
	if tr.cfg.SessionID == "" {
		t.Error("SessionID should be auto-generated when empty")
	}
	// Role should default to "admin"
	if tr.cfg.Role != "admin" {
		t.Errorf("expected default role 'admin', got %s", tr.cfg.Role)
	}
}

func TestNewTransport_CustomConfig(t *testing.T) {
	cfg := Config{
		ProxyURL:  "http://proxy:3000",
		APIKey:    "sk-test-key",
		Role:      "viewer",
		SessionID: "custom-session-123",
	}
	tr := NewTransport(cfg, nil)
	if tr.cfg.SessionID != "custom-session-123" {
		t.Error("should preserve custom SessionID")
	}
	if tr.cfg.Role != "viewer" {
		t.Error("should preserve custom Role")
	}
}

func TestNewTransport_CustomBase(t *testing.T) {
	base := &http.Transport{}
	tr := NewTransport(Config{}, base)
	if tr.base != base {
		t.Error("should use provided base transport")
	}
}

func TestRoundTrip_InjectsHeaders(t *testing.T) {
	// Use a test server to capture the request
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := Config{
		ProxyURL:  server.URL,
		APIKey:    "sk-test-12345",
		Role:      "admin",
		SessionID: "sess-abc",
	}
	tr := NewTransport(cfg, nil)

	req, _ := http.NewRequest("GET", server.URL+"/v1/models", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if capturedHeaders.Get("X-Session-ID") != "sess-abc" {
		t.Errorf("expected X-Session-ID=sess-abc, got %s", capturedHeaders.Get("X-Session-ID"))
	}
	if capturedHeaders.Get("X-User-Role") != "admin" {
		t.Errorf("expected X-User-Role=admin, got %s", capturedHeaders.Get("X-User-Role"))
	}
	if capturedHeaders.Get("Authorization") != "Bearer sk-test-12345" {
		t.Errorf("expected Authorization header, got %s", capturedHeaders.Get("Authorization"))
	}
}

func TestRoundTrip_PreservesExistingAuth(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer server.Close()

	cfg := Config{
		ProxyURL: server.URL,
		APIKey:   "should-not-override",
	}
	tr := NewTransport(cfg, nil)

	req, _ := http.NewRequest("GET", server.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer original-key")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer resp.Body.Close()

	if capturedAuth != "Bearer original-key" {
		t.Errorf("should preserve existing Authorization, got %s", capturedAuth)
	}
}

func TestRoundTrip_NoAPIKey(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer server.Close()

	cfg := Config{
		ProxyURL: server.URL,
		// No APIKey
	}
	tr := NewTransport(cfg, nil)

	req, _ := http.NewRequest("GET", server.URL+"/v1/models", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer resp.Body.Close()

	if capturedAuth != "" {
		t.Errorf("should not set Authorization when APIKey is empty, got %s", capturedAuth)
	}
}

func TestNewHTTPClient(t *testing.T) {
	cfg := Config{
		ProxyURL: "http://localhost:8080",
		APIKey:   "test-key",
	}
	client := NewHTTPClient(cfg)
	if client == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
	if client.Transport == nil {
		t.Error("Transport should be set")
	}
}

func TestNewHTTPClient_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Agent Veil headers are present
		if r.Header.Get("X-Session-ID") == "" {
			t.Error("missing X-Session-ID")
		}
		if r.Header.Get("X-User-Role") == "" {
			t.Error("missing X-User-Role")
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewHTTPClient(Config{
		ProxyURL: server.URL,
		APIKey:   "test-key",
		Role:     "viewer",
	})

	resp, err := client.Get(server.URL + "/v1/models")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		ProxyURL:  "http://proxy",
		APIKey:    "key",
		Role:      "admin",
		SessionID: "sess",
	}

	if cfg.ProxyURL != "http://proxy" {
		t.Error("ProxyURL mismatch")
	}
	if cfg.APIKey != "key" {
		t.Error("APIKey mismatch")
	}
}
